package a2aapp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"iter"

	a2a "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"

	"github.com/jamespud/magi/backend/application/audit"
	"github.com/jamespud/magi/backend/application/auth"
	"github.com/jamespud/magi/backend/application/decision"
	"github.com/jamespud/magi/backend/application/metrics"
	"github.com/jamespud/magi/backend/application/tracing"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

// StreamProjector yields ordered A2A events for an owner-scoped task. The
// durable stream projector implements it in the streaming milestone.
type StreamProjector interface {
	Events(ctx context.Context, userID int64, taskID string) iter.Seq2[a2a.Event, error]
}

// Handler implements the official a2asrv.RequestHandler over MAGI's durable
// submission repository. It never writes to an SDK task store; MAGI remains
// the only execution state machine.
type Handler struct {
	submissions *SubmissionService
	repository  SubmissionRepository
	projector   *TaskProjector
	cursor      CursorCodec
	runManager  *decision.RunManager
	stream      StreamProjector
	live        port.LiveEventPublisher
	metrics     *metrics.Registry
	audit       *audit.Service
}

// HandlerOption configures optional observability on a Handler.
type HandlerOption func(*Handler)

// WithHandlerMetrics enables bounded A2A request metrics.
func WithHandlerMetrics(reg *metrics.Registry) HandlerOption {
	return func(h *Handler) { h.metrics = reg }
}

// WithHandlerAudit records Send/Cancel audit events (message IDs are hashed).
func WithHandlerAudit(auditSvc *audit.Service) HandlerOption {
	return func(h *Handler) { h.audit = auditSvc }
}

// WithHandlerLivePublisher fans out events that the repository already made
// durable, so in-process streams observe cancellations immediately.
func WithHandlerLivePublisher(live port.LiveEventPublisher) HandlerOption {
	return func(h *Handler) { h.live = live }
}

func NewHandler(submissions *SubmissionService, repository SubmissionRepository, projector *TaskProjector, cursor CursorCodec, runManager *decision.RunManager, stream StreamProjector, opts ...HandlerOption) *Handler {
	h := &Handler{submissions: submissions, repository: repository, projector: projector, cursor: cursor, runManager: runManager, stream: stream}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

var _ a2asrv.RequestHandler = (*Handler)(nil)

// GetTask returns the owner-scoped Task snapshot.
func (h *Handler) GetTask(ctx context.Context, req *a2a.GetTaskRequest) (_ *a2a.Task, err error) {
	ctx, span := tracing.Start(ctx, "a2a.GetTask")
	defer span.End()
	defer h.finish(metrics.A2AOperationGetTask, &err)
	if req == nil || req.ID == "" {
		return nil, a2a.NewError(a2a.ErrInvalidParams, "task id is required")
	}
	userID := userIDFrom(ctx)
	span.SetAttributes(attribute.String("task.id", string(req.ID)), attribute.Int64("user.id", userID))
	record, err := h.repository.GetTaskRecord(ctx, userID, string(req.ID))
	if errors.Is(err, ErrNotFound) {
		return nil, a2a.NewError(a2a.ErrTaskNotFound, "task not found")
	}
	if err != nil {
		return nil, h.internalError(err)
	}
	return h.projector.Project(record, historyLength(req.HistoryLength), true), nil
}

// ListTasks lists owner-scoped tasks with keyset pagination.
func (h *Handler) ListTasks(ctx context.Context, req *a2a.ListTasksRequest) (_ *a2a.ListTasksResponse, err error) {
	ctx, span := tracing.Start(ctx, "a2a.ListTasks")
	defer span.End()
	defer h.finish(metrics.A2AOperationListTasks, &err)
	if req == nil {
		return nil, a2a.NewError(a2a.ErrInvalidParams, "list request is required")
	}
	userID := userIDFrom(ctx)
	span.SetAttributes(attribute.Int64("user.id", userID))
	if req.Status != "" && !IsSupportedListState(req.Status) {
		return nil, a2a.NewError(a2a.ErrInvalidParams, "unsupported task state")
	}
	pageSize, err := h.cursor.ValidatePageSize(req.PageSize)
	if err != nil {
		return nil, a2a.NewError(a2a.ErrInvalidParams, err.Error())
	}
	filter := TaskListFilter{
		UserID:               userID,
		ContextID:            req.ContextID,
		Status:               string(req.Status),
		Limit:                pageSize,
		HistoryLength:        historyLength(req.HistoryLength),
		IncludeArtifacts:     req.IncludeArtifacts,
		StatusTimestampAfter: req.StatusTimestampAfter,
	}
	if req.PageToken != "" {
		after, err := h.cursor.Decode(req.PageToken)
		if err != nil {
			return nil, a2a.NewError(a2a.ErrInvalidParams, "invalid page token")
		}
		filter.After = &after
	}
	page, err := h.repository.ListTasks(ctx, filter)
	if err != nil {
		return nil, h.internalError(err)
	}
	response := &a2a.ListTasksResponse{
		Tasks:     make([]*a2a.Task, 0, len(page.Records)),
		TotalSize: page.Total,
		PageSize:  pageSize,
	}
	for i := range page.Records {
		response.Tasks = append(response.Tasks, h.projector.Project(&page.Records[i], filter.HistoryLength, filter.IncludeArtifacts))
	}
	if page.Next != nil {
		token, err := h.cursor.Encode(*page.Next)
		if err != nil {
			return nil, h.internalError(err)
		}
		response.NextPageToken = token
	}
	return response, nil
}

// CancelTask atomically cancels the durable job and conditionally marks the
// Case CANCELLED, then cancels any local worker handle.
func (h *Handler) CancelTask(ctx context.Context, req *a2a.CancelTaskRequest) (_ *a2a.Task, err error) {
	ctx, span := tracing.Start(ctx, "a2a.CancelTask")
	defer span.End()
	defer h.finish(metrics.A2AOperationCancelTask, &err)
	if req == nil || req.ID == "" {
		return nil, a2a.NewError(a2a.ErrInvalidParams, "task id is required")
	}
	userID := userIDFrom(ctx)
	span.SetAttributes(attribute.String("task.id", string(req.ID)), attribute.Int64("user.id", userID))
	result, err := h.repository.CancelTask(ctx, userID, string(req.ID))
	if err != nil {
		return nil, h.internalError(err)
	}
	switch result.Outcome {
	case CancelNotFound:
		return nil, a2a.NewError(a2a.ErrTaskNotFound, "task not found")
	case CancelNotCancelable:
		return nil, a2a.NewError(a2a.ErrTaskNotCancelable, "task is in a terminal state")
	case CancelApplied, CancelAlreadyCanceled:
		if result.Event != nil && h.live != nil {
			// The event is already durable; this is a best-effort live fanout
			// for in-process streams. Remote replicas read the durable event.
			_ = h.live.PublishLive(ctx, *result.Event)
		}
		h.runManager.CancelLocal(string(req.ID))
		task := h.projector.Project(result.Record, 1, true)
		h.auditCancel(ctx, req, task.Status.State == a2a.TaskStateCanceled)
		return task, nil
	default:
		return nil, a2a.NewError(a2a.ErrInternalError, "unexpected cancel outcome")
	}
}

// SendMessage durably submits a task and returns the current Task snapshot.
func (h *Handler) SendMessage(ctx context.Context, req *a2a.SendMessageRequest) (_ a2a.SendMessageResult, err error) {
	ctx, span := tracing.Start(ctx, "a2a.SendMessage")
	defer span.End()
	defer h.finish(metrics.A2AOperationSendMessage, &err)
	userID := userIDFrom(ctx)
	span.SetAttributes(attribute.Int64("user.id", userID))
	task, err := h.submissions.Submit(ctx, userID, req)
	if err != nil {
		return nil, h.mapSubmitError(err)
	}
	h.auditSend(ctx, task, userID)
	return task, nil
}

// SendStreamingMessage durably submits then delegates to the stream projector.
func (h *Handler) SendStreamingMessage(ctx context.Context, req *a2a.SendMessageRequest) iter.Seq2[a2a.Event, error] {
	if h.metrics != nil {
		h.metrics.IncA2ARequest(metrics.A2AOperationSendStreaming, metrics.A2AResultOK)
	}
	return func(yield func(a2a.Event, error) bool) {
		if h.stream == nil {
			yield(nil, a2a.ErrUnsupportedOperation)
			return
		}
		task, err := h.submissions.Submit(ctx, userIDFrom(ctx), req)
		if err != nil {
			yield(nil, h.mapSubmitError(err))
			return
		}
		for event, streamErr := range h.stream.Events(ctx, userIDFrom(ctx), string(task.ID)) {
			if !yield(event, streamErr) {
				return
			}
		}
	}
}

// SubscribeToTask delegates to the stream projector after owner validation.
func (h *Handler) SubscribeToTask(ctx context.Context, req *a2a.SubscribeToTaskRequest) iter.Seq2[a2a.Event, error] {
	if h.metrics != nil {
		h.metrics.IncA2ARequest(metrics.A2AOperationSubscribeToTask, metrics.A2AResultOK)
	}
	return func(yield func(a2a.Event, error) bool) {
		if h.stream == nil {
			yield(nil, a2a.ErrUnsupportedOperation)
			return
		}
		if req == nil || req.ID == "" {
			yield(nil, a2a.NewError(a2a.ErrInvalidParams, "task id is required"))
			return
		}
		for event, streamErr := range h.stream.Events(ctx, userIDFrom(ctx), string(req.ID)) {
			if !yield(event, streamErr) {
				return
			}
		}
	}
}

// finish records the bounded request metric from a named error return.
func (h *Handler) finish(op metrics.A2AOperation, err *error) {
	if h.metrics == nil {
		return
	}
	res := metrics.A2AResultOK
	if *err != nil {
		res = metrics.A2AResultError
	}
	h.metrics.IncA2ARequest(op, res)
}

func (h *Handler) auditSend(ctx context.Context, task *a2a.Task, userID int64) {
	if h.audit == nil || task == nil {
		return
	}
	h.recordAudit(ctx, "a2a.send", string(task.ID), messageIDFromTask(task), userID, 200)
}

func (h *Handler) auditCancel(ctx context.Context, req *a2a.CancelTaskRequest, ok bool) {
	if h.audit == nil || req == nil {
		return
	}
	status := 200
	if !ok {
		status = 409
	}
	h.recordAudit(ctx, "a2a.cancel", string(req.ID), "", userIDFrom(ctx), status)
}

func (h *Handler) recordAudit(ctx context.Context, action, taskID, messageID string, userID int64, status int) {
	p := auth.PrincipalFrom(ctx)
	detail := "trace=" + uuid.NewString()
	if messageID != "" {
		detail += " message=" + hashID(messageID)
	}
	event := &entity.AuditEvent{UserID: userID, Action: action, Resource: taskID, Detail: detail, Status: status}
	if p != nil {
		event.Username = p.Name
		event.Role = p.Role
	}
	_ = h.audit.Record(ctx, event)
}

func messageIDFromTask(task *a2a.Task) string {
	if task == nil || len(task.History) == 0 {
		return ""
	}
	return task.History[0].ID
}

func hashID(id string) string {
	sum := sha256.Sum256([]byte(id))
	return hex.EncodeToString(sum[:8])
}

func (h *Handler) GetTaskPushConfig(ctx context.Context, req *a2a.GetTaskPushConfigRequest) (*a2a.PushConfig, error) {
	return nil, a2a.ErrPushNotificationNotSupported
}

func (h *Handler) ListTaskPushConfigs(ctx context.Context, req *a2a.ListTaskPushConfigRequest) (*a2a.ListTaskPushConfigResponse, error) {
	return nil, a2a.ErrPushNotificationNotSupported
}

func (h *Handler) CreateTaskPushConfig(ctx context.Context, req *a2a.PushConfig) (*a2a.PushConfig, error) {
	return nil, a2a.ErrPushNotificationNotSupported
}

func (h *Handler) DeleteTaskPushConfig(ctx context.Context, req *a2a.DeleteTaskPushConfigRequest) error {
	return a2a.ErrPushNotificationNotSupported
}

func (h *Handler) GetExtendedAgentCard(ctx context.Context, req *a2a.GetExtendedAgentCardRequest) (*a2a.AgentCard, error) {
	return nil, a2a.ErrExtendedCardNotConfigured
}

func userIDFrom(ctx context.Context) int64 {
	if principal := auth.PrincipalFrom(ctx); principal != nil {
		return principal.UserID
	}
	return 0
}

func historyLength(length *int) int {
	if length != nil && *length > 0 {
		return 1
	}
	return 0
}

func (h *Handler) mapSubmitError(err error) error {
	switch {
	case errors.Is(err, ErrInvalidInput):
		return a2a.NewError(a2a.ErrInvalidParams, err.Error())
	case errors.Is(err, ErrContentTypeNotSupported):
		return a2a.NewError(a2a.ErrUnsupportedContentType, err.Error())
	case errors.Is(err, ErrTaskMessageNotSupported):
		return a2a.NewError(a2a.ErrUnsupportedOperation, err.Error())
	case errors.Is(err, ErrIdempotencyConflict):
		return a2a.NewError(a2a.ErrInvalidRequest, err.Error())
	case errors.Is(err, ErrForbidden):
		return a2a.NewError(a2a.ErrTaskNotFound, "task or context not found")
	default:
		return h.internalError(err)
	}
}

func (h *Handler) internalError(err error) error {
	return a2a.NewError(a2a.ErrInternalError, "request failed (trace "+uuid.NewString()+")")
}
