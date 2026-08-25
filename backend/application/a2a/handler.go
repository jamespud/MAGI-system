package a2aapp

import (
	"context"
	"errors"
	"iter"

	a2a "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/google/uuid"
	"github.com/jamespud/magi/backend/application/auth"
	"github.com/jamespud/magi/backend/application/decision"
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
}

func NewHandler(submissions *SubmissionService, repository SubmissionRepository, projector *TaskProjector, cursor CursorCodec, runManager *decision.RunManager, stream StreamProjector) *Handler {
	return &Handler{submissions: submissions, repository: repository, projector: projector, cursor: cursor, runManager: runManager, stream: stream}
}

var _ a2asrv.RequestHandler = (*Handler)(nil)

// GetTask returns the owner-scoped Task snapshot.
func (h *Handler) GetTask(ctx context.Context, req *a2a.GetTaskRequest) (*a2a.Task, error) {
	if req == nil || req.ID == "" {
		return nil, a2a.NewError(a2a.ErrInvalidParams, "task id is required")
	}
	record, err := h.repository.GetTaskRecord(ctx, userIDFrom(ctx), string(req.ID))
	if errors.Is(err, ErrNotFound) {
		return nil, a2a.NewError(a2a.ErrTaskNotFound, "task not found")
	}
	if err != nil {
		return nil, h.internalError(err)
	}
	return h.projector.Project(record, historyLength(req.HistoryLength)), nil
}

// ListTasks lists owner-scoped tasks with keyset pagination.
func (h *Handler) ListTasks(ctx context.Context, req *a2a.ListTasksRequest) (*a2a.ListTasksResponse, error) {
	if req == nil {
		return nil, a2a.NewError(a2a.ErrInvalidParams, "list request is required")
	}
	pageSize, err := h.cursor.ValidatePageSize(req.PageSize)
	if err != nil {
		return nil, a2a.NewError(a2a.ErrInvalidParams, err.Error())
	}
	filter := TaskListFilter{
		UserID:               userIDFrom(ctx),
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
		response.Tasks = append(response.Tasks, h.projector.Project(&page.Records[i], filter.HistoryLength))
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
func (h *Handler) CancelTask(ctx context.Context, req *a2a.CancelTaskRequest) (*a2a.Task, error) {
	if req == nil || req.ID == "" {
		return nil, a2a.NewError(a2a.ErrInvalidParams, "task id is required")
	}
	record, outcome, err := h.repository.CancelTask(ctx, userIDFrom(ctx), string(req.ID))
	if err != nil {
		return nil, h.internalError(err)
	}
	switch outcome {
	case CancelNotFound:
		return nil, a2a.NewError(a2a.ErrTaskNotFound, "task not found")
	case CancelNotCancelable:
		return nil, a2a.NewError(a2a.ErrTaskNotCancelable, "task is in a terminal state")
	case CancelApplied, CancelAlreadyCanceled:
		h.runManager.CancelLocal(string(req.ID))
		return h.projector.Project(record, 1), nil
	default:
		return nil, a2a.NewError(a2a.ErrInternalError, "unexpected cancel outcome")
	}
}

// SendMessage durably submits a task and returns the current Task snapshot.
func (h *Handler) SendMessage(ctx context.Context, req *a2a.SendMessageRequest) (a2a.SendMessageResult, error) {
	task, err := h.submissions.Submit(ctx, userIDFrom(ctx), req)
	if err != nil {
		return nil, h.mapSubmitError(err)
	}
	return task, nil
}

// SendStreamingMessage durably submits then delegates to the stream projector.
func (h *Handler) SendStreamingMessage(ctx context.Context, req *a2a.SendMessageRequest) iter.Seq2[a2a.Event, error] {
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
