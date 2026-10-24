package magi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/jamespud/magi/backend/domain/canonical"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/inputartifact"
	"github.com/jamespud/magi/backend/domain/port"
	"github.com/jamespud/magi/backend/domain/readsetjournal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ReadSetJournalRepository is the one local material-registration ingress for
// this staged slice. Register durably commits exact canonical pre-exposure
// facts before it returns. It never calls a provider and provides no apply,
// authoritative-sequence, or APPLIED_VERIFIED transition.
type ReadSetJournalRepository struct {
	db     *gorm.DB
	config readsetjournal.Config
}

// ReadSetJournalRegistration is a committed local registration or an exact
// same-ID/same-bytes replay. Its provider state is separate from registration.
type ReadSetJournalRegistration struct {
	JournalID string
	Sequence  int64
	Created   bool
	Event     readsetjournal.EventFact
}

func NewReadSetJournalRepository(db *gorm.DB, cfg readsetjournal.Config) *ReadSetJournalRepository {
	return &ReadSetJournalRepository{db: db, config: cfg.Normalized()}
}

// Register validates and atomically appends one canonical registration, its
// exact dependency nodes/edges and its PENDING delivery row. The caller must not
// expose material until this method returns success or recovers an exact
// duplicate by delivery ID.
func (r *ReadSetJournalRepository) Register(ctx context.Context, owner *entity.ExecutionContext, req readsetjournal.RegisterRequest) (ReadSetJournalRegistration, error) {
	if r == nil || !r.config.Enabled {
		return ReadSetJournalRegistration{}, readsetjournal.ErrDisabled
	}
	if r.db == nil || owner == nil || !owner.IsDurable() {
		return ReadSetJournalRegistration{}, port.ErrLeaseLost
	}
	if !adapterOwnerMatches(owner, req.Owner) {
		return ReadSetJournalRegistration{}, readsetjournal.ErrOwnerFence
	}
	prepared, err := readsetjournal.PrepareRegistration(r.config, req)
	if err != nil {
		return ReadSetJournalRegistration{}, err
	}

	var result ReadSetJournalRegistration
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		job, _, err := lockActiveExecution(tx, owner)
		if err != nil {
			return err
		}
		if job.LeaseUntil == nil {
			return port.ErrLeaseLost
		}
		leaseUntil := *job.LeaseUntil

		journal, err := lockOrCreateJournal(tx, prepared, owner, leaseUntil)
		if err != nil {
			return err
		}
		if journal.State != string(readsetjournal.JournalOpen) {
			return fmt.Errorf("%w: journal is %s", readsetjournal.ErrJournalScope, journal.State)
		}
		storedScope := scopeFromJournalModel(journal)
		if storedScope != prepared.Scope {
			return fmt.Errorf("%w: admission scope differs from durable journal", readsetjournal.ErrJournalScope)
		}

		var existing ReadSetJournalEventModel
		eventErr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("journal_id = ? AND delivery_id = ?", journal.ID, prepared.Event.DeliveryID).
			First(&existing).Error
		switch {
		case eventErr == nil:
			if existing.PayloadDigest != prepared.EventDigest || existing.RequestDigest != prepared.RequestDigest ||
				!bytes.Equal(existing.PayloadJSON, prepared.EventBytes) {
				return readsetjournal.ErrDeliveryConflict
			}
			state, err := deliveryState(tx, journal.ID, prepared.Event.DeliveryID)
			if err != nil {
				return err
			}
			fact, err := eventFactFromModels(&existing, state)
			if err != nil {
				return err
			}
			result = ReadSetJournalRegistration{JournalID: journal.ID, Sequence: existing.LocalSequence, Created: false, Event: fact}
			return nil
		case !errors.Is(eventErr, gorm.ErrRecordNotFound):
			return eventErr
		}

		var providerReuse int64
		if err := tx.Model(&ReadSetJournalEventModel{}).
			Where("journal_id = ? AND provider_request_id = ?", journal.ID, prepared.Event.ProviderRequestID).
			Count(&providerReuse).Error; err != nil {
			return err
		}
		if providerReuse != 0 {
			return fmt.Errorf("%w: provider request reused by another delivery", readsetjournal.ErrDeliveryConflict)
		}
		if journal.HighWatermark >= int64(r.config.MaxEventsPerJournal) {
			return fmt.Errorf("%w: journal event count ceiling", readsetjournal.ErrLimit)
		}
		if journal.EventBytes+int64(len(prepared.EventBytes)) > r.config.MaxJournalBytes {
			return fmt.Errorf("%w: journal aggregate byte ceiling", readsetjournal.ErrLimit)
		}
		nextSequence := journal.HighWatermark + 1
		now := time.Now().UTC()

		for _, preparedNode := range prepared.Nodes {
			if err := upsertExactNode(tx, journal.ID, preparedNode, nextSequence, owner, prepared.Scope, now); err != nil {
				return err
			}
		}
		for _, preparedNode := range prepared.Nodes {
			for _, parentID := range preparedNode.Node.ParentNodeIDs {
				edge := ReadSetJournalEdgeModel{
					JournalID: journal.ID, ChildNodeID: preparedNode.Node.NodeID,
					ParentNodeID: parentID, CreatedSequence: nextSequence,
				}
				if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&edge).Error; err != nil {
					return err
				}
			}
		}

		event := ReadSetJournalEventModel{
			JournalID: journal.ID, LocalSequence: nextSequence,
			EventVersion: prepared.Event.EventVersion, DeliveryID: prepared.Event.DeliveryID,
			ProviderRequestID:   prepared.Event.ProviderRequestID,
			RegistrationState:   string(readsetjournal.RegistrationRegistered),
			ExecutionGeneration: owner.ExecutionGeneration,
			OwnerJobID:          owner.JobID, OwnerWorkerID: owner.WorkerID, OwnerLeaseUntil: leaseUntil,
			SourceNodeID: prepared.Event.Source.NodeID, SourceVersion: prepared.Event.Source.SourceVersion,
			TargetPersona: prepared.Event.TargetPersona, PayloadJSON: append([]byte(nil), prepared.EventBytes...),
			PayloadDigest: prepared.EventDigest, RequestDigest: prepared.RequestDigest,
			PayloadBytes: len(prepared.EventBytes), RegisteredAt: now,
		}
		if err := tx.Create(&event).Error; err != nil {
			return err
		}
		delivery := ReadSetJournalDeliveryModel{
			JournalID: journal.ID, DeliveryID: prepared.Event.DeliveryID, EventSequence: nextSequence,
			ProviderRequestID: prepared.Event.ProviderRequestID,
			State:             string(readsetjournal.ProviderPending), StateVersion: 0,
			FirstExecutionGeneration: owner.ExecutionGeneration,
			LastExecutionGeneration:  owner.ExecutionGeneration,
			LastOwnerJobID:           owner.JobID,
			LastOwnerWorkerID:        owner.WorkerID,
			LastOwnerLeaseUntil:      leaseUntil,
			PayloadDigest:            prepared.EventDigest, LastReason: "",
		}
		if err := tx.Create(&delivery).Error; err != nil {
			return err
		}

		update := tx.Model(&ReadSetJournalModel{}).
			Where("id = ? AND high_watermark = ? AND state_version = ?", journal.ID, journal.HighWatermark, journal.StateVersion).
			Updates(map[string]any{
				"high_watermark":       nextSequence,
				"pending_count":        journal.PendingCount + 1,
				"event_bytes":          journal.EventBytes + int64(len(prepared.EventBytes)),
				"owner_job_id":         owner.JobID,
				"owner_worker_id":      owner.WorkerID,
				"execution_generation": owner.ExecutionGeneration,
				"owner_lease_until":    leaseUntil,
				"state_version":        journal.StateVersion + 1,
				"updated_at":           now,
			})
		if update.Error != nil {
			return update.Error
		}
		if update.RowsAffected != 1 {
			return fmt.Errorf("%w: journal high-watermark CAS failed", readsetjournal.ErrJournalScope)
		}
		fact, err := eventFactFromModels(&event, &delivery)
		if err != nil {
			return err
		}
		result = ReadSetJournalRegistration{JournalID: journal.ID, Sequence: nextSequence, Created: true, Event: fact}
		return nil
	})
	if err != nil {
		return ReadSetJournalRegistration{}, err
	}
	return result, nil
}

// ReadJournal performs a bounded, current-owner read and verifies the
// continuous local prefix plus exact canonical payload bytes. A changed scoped
// epoch/ID rejects the read before any cached material can be restored.
func (r *ReadSetJournalRepository) ReadJournal(ctx context.Context, owner *entity.ExecutionContext, key readsetjournal.JournalKey, current readsetjournal.AdmissionScope) (readsetjournal.JournalView, error) {
	if r == nil || !r.config.Enabled {
		return readsetjournal.JournalView{}, readsetjournal.ErrDisabled
	}
	if r.db == nil || owner == nil || !owner.IsDurable() {
		return readsetjournal.JournalView{}, port.ErrLeaseLost
	}
	if err := validateJournalReadTarget(owner, key); err != nil {
		return readsetjournal.JournalView{}, err
	}
	journalID, err := readsetjournal.JournalIDFor(key)
	if err != nil {
		return readsetjournal.JournalView{}, err
	}
	var view readsetjournal.JournalView
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, _, err := lockActiveExecution(tx, owner); err != nil {
			return err
		}
		var journal ReadSetJournalModel
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("case_id = ? AND round_no = ? AND context_manifest_id = ? AND persona_id = ?",
				key.CaseID, key.Round, key.ContextManifestID, key.PersonaID).First(&journal).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return readsetjournal.ErrNotFound
		}
		if err != nil {
			return err
		}
		if journal.ID != journalID {
			return fmt.Errorf("%w: journal ID differs from frozen key", readsetjournal.ErrInvalidJournal)
		}
		switch readsetjournal.JournalState(journal.State) {
		case readsetjournal.JournalOpen, readsetjournal.JournalQuiescing, readsetjournal.JournalInputClosed:
		default:
			return fmt.Errorf("%w: invalid journal lifecycle state %q", readsetjournal.ErrInvalidJournal, journal.State)
		}
		if journal.HighWatermark < 0 || journal.HighWatermark > int64(r.config.MaxEventsPerJournal) ||
			journal.EventBytes < 0 || journal.EventBytes > r.config.MaxJournalBytes || journal.PendingCount < 0 ||
			journal.PendingCount > journal.HighWatermark {
			return fmt.Errorf("%w: journal counters exceed configured bounds", readsetjournal.ErrInvalidJournal)
		}
		storedScope := scopeFromJournalModel(&journal)
		if err := readsetjournal.ValidateCurrentScope(storedScope, current); err != nil {
			return err
		}
		var deliveries []ReadSetJournalDeliveryModel
		if err := tx.Where("journal_id = ?", journal.ID).Order("event_sequence asc").
			Limit(r.config.MaxEventsPerJournal + 1).Find(&deliveries).Error; err != nil {
			return err
		}
		if len(deliveries) != int(journal.HighWatermark) {
			return fmt.Errorf("%w: delivery count does not equal high watermark", readsetjournal.ErrInvalidJournal)
		}
		deliveryBySequence := make(map[int64]*ReadSetJournalDeliveryModel, len(deliveries))
		var pending int64
		for i := range deliveries {
			delivery := &deliveries[i]
			deliveryBySequence[delivery.EventSequence] = delivery
			switch readsetjournal.ProviderState(delivery.State) {
			case readsetjournal.ProviderPending, readsetjournal.ProviderUnknown, readsetjournal.ProviderUnverifiable:
				pending++
			case readsetjournal.ProviderApplied:
				if delivery.AuthoritySequence == nil || delivery.VerificationRecordJSON == nil || delivery.InjectedAt == nil || delivery.VerifiedAt == nil {
					return readsetjournal.ErrAppliedUnverified
				}
			case readsetjournal.ProviderFailed:
			default:
				return fmt.Errorf("%w: invalid provider state %q", readsetjournal.ErrInvalidJournal, delivery.State)
			}
		}
		if pending != journal.PendingCount {
			return fmt.Errorf("%w: pending count mismatch", readsetjournal.ErrInvalidJournal)
		}
		var events []ReadSetJournalEventModel
		if err := tx.Where("journal_id = ?", journal.ID).Order("local_sequence asc").
			Limit(r.config.MaxEventsPerJournal + 1).Find(&events).Error; err != nil {
			return err
		}
		if len(events) != int(journal.HighWatermark) {
			return fmt.Errorf("%w: event count does not equal high watermark", readsetjournal.ErrInvalidJournal)
		}
		facts := make([]readsetjournal.EventFact, len(events))
		plans := make([]readsetjournal.RegistrationPlan, len(events))
		var eventBytes int64
		for i := range events {
			event := &events[i]
			if event.LocalSequence != int64(i+1) {
				return fmt.Errorf("%w: non-continuous local sequence", readsetjournal.ErrInvalidJournal)
			}
			if _, err := readsetjournal.AdoptRegistrationEvent(event.PayloadJSON); err != nil {
				return err
			}
			sum := sha256.Sum256(event.PayloadJSON)
			if hex.EncodeToString(sum[:]) != event.PayloadDigest {
				return fmt.Errorf("%w: stored event payload digest mismatch", readsetjournal.ErrInvalidJournal)
			}
			plan, err := readsetjournal.DecodeRegistrationEvent(event.PayloadJSON)
			if err != nil {
				return err
			}
			if plan.EventVersion != event.EventVersion || plan.ExecutionGeneration != event.ExecutionGeneration ||
				plan.Source.NodeID != event.SourceNodeID || plan.Source.SourceVersion != event.SourceVersion ||
				plan.TargetPersona != event.TargetPersona || plan.DeliveryID != event.DeliveryID ||
				plan.ProviderRequestID != event.ProviderRequestID || plan.JournalKey != key ||
				plan.AdmissionScope != storedScope {
				return fmt.Errorf("%w: stored event columns differ from canonical payload", readsetjournal.ErrInvalidJournal)
			}
			plans[i] = plan
			eventBytes += int64(len(event.PayloadJSON))
			delivery := deliveryBySequence[event.LocalSequence]
			if delivery == nil || delivery.DeliveryID != event.DeliveryID || delivery.PayloadDigest != event.PayloadDigest {
				return fmt.Errorf("%w: event/delivery binding mismatch", readsetjournal.ErrInvalidJournal)
			}
			fact, err := eventFactFromModels(event, delivery)
			if err != nil {
				return err
			}
			facts[i] = fact
		}
		if eventBytes != journal.EventBytes {
			return fmt.Errorf("%w: journal event byte count mismatch", readsetjournal.ErrInvalidJournal)
		}
		var nodeModels []ReadSetJournalNodeModel
		if err := tx.Where("journal_id = ?", journal.ID).Order("node_id asc").
			Limit(r.config.MaxNodes + 1).Find(&nodeModels).Error; err != nil {
			return err
		}
		if len(nodeModels) > r.config.MaxNodes {
			return fmt.Errorf("%w: persisted node count exceeds ceiling", readsetjournal.ErrLimit)
		}
		nodes := make([]inputartifact.SourceNode, len(nodeModels))
		for i := range nodeModels {
			node, err := decodeStoredNode(&nodeModels[i])
			if err != nil {
				return err
			}
			nodes[i] = node
		}
		maxEdges := r.config.MaxNodes * r.config.MaxParents
		var edges []ReadSetJournalEdgeModel
		if err := tx.Where("journal_id = ?", journal.ID).Order("parent_node_id asc, child_node_id asc").
			Limit(maxEdges + 1).Find(&edges).Error; err != nil {
			return err
		}
		if len(edges) > maxEdges {
			return fmt.Errorf("%w: persisted edge count exceeds ceiling", readsetjournal.ErrLimit)
		}
		domainEdges := make([]readsetjournal.DependencyEdge, len(edges))
		for i, edge := range edges {
			domainEdges[i] = readsetjournal.DependencyEdge{ParentNodeID: edge.ParentNodeID, ChildNodeID: edge.ChildNodeID}
		}
		if err := readsetjournal.ValidateJournalGraph(plans, nodes, domainEdges); err != nil {
			return err
		}
		reverse := make(map[string][]string)
		for _, edge := range edges {
			reverse[edge.ParentNodeID] = append(reverse[edge.ParentNodeID], edge.ChildNodeID)
		}
		for parent := range reverse {
			sort.Strings(reverse[parent])
		}
		view = readsetjournal.JournalView{
			JournalID: journal.ID, JournalKey: key, State: readsetjournal.JournalState(journal.State),
			HighWatermark: journal.HighWatermark, PendingCount: journal.PendingCount,
			Scope: storedScope, Events: facts, Nodes: nodes, ReverseDependents: reverse,
		}
		return nil
	})
	if err != nil {
		return readsetjournal.JournalView{}, err
	}
	return view, nil
}

// MarkProviderUnknown records an ambiguous/timeout observation. It retains the
// pending count because exposure may have happened, never creates a read event,
// never decrements budget, and never resends.
func (r *ReadSetJournalRepository) MarkProviderUnknown(ctx context.Context, owner *entity.ExecutionContext, key readsetjournal.JournalKey, current readsetjournal.AdmissionScope, deliveryID, reason string) (readsetjournal.EventFact, error) {
	return r.markUnverified(ctx, owner, key, current, deliveryID, readsetjournal.ProviderUnknown, reason)
}

// MarkProviderUnverifiable records that no independent provider/session source
// exists to reconcile an ambiguous delivery. It remains non-authoritative.
func (r *ReadSetJournalRepository) MarkProviderUnverifiable(ctx context.Context, owner *entity.ExecutionContext, key readsetjournal.JournalKey, current readsetjournal.AdmissionScope, deliveryID, reason string) (readsetjournal.EventFact, error) {
	return r.markUnverified(ctx, owner, key, current, deliveryID, readsetjournal.ProviderUnverifiable, reason)
}

func (r *ReadSetJournalRepository) markUnverified(ctx context.Context, owner *entity.ExecutionContext, key readsetjournal.JournalKey, current readsetjournal.AdmissionScope, deliveryID string, target readsetjournal.ProviderState, reason string) (readsetjournal.EventFact, error) {
	if r == nil || !r.config.Enabled {
		return readsetjournal.EventFact{}, readsetjournal.ErrDisabled
	}
	if r.db == nil || owner == nil || !owner.IsDurable() {
		return readsetjournal.EventFact{}, port.ErrLeaseLost
	}
	if err := validateJournalReadTarget(owner, key); err != nil {
		return readsetjournal.EventFact{}, err
	}
	journalID, err := readsetjournal.JournalIDFor(key)
	if err != nil {
		return readsetjournal.EventFact{}, err
	}
	if target != readsetjournal.ProviderUnknown && target != readsetjournal.ProviderUnverifiable {
		return readsetjournal.EventFact{}, readsetjournal.ErrProviderBlocked
	}
	if len(reason) > 4096 {
		return readsetjournal.EventFact{}, fmt.Errorf("%w: provider reason exceeds ceiling", readsetjournal.ErrLimit)
	}
	var fact readsetjournal.EventFact
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		job, _, err := lockActiveExecution(tx, owner)
		if err != nil {
			return err
		}
		if job.LeaseUntil == nil {
			return port.ErrLeaseLost
		}
		leaseUntil := *job.LeaseUntil
		var journal ReadSetJournalModel
		err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("case_id = ? AND round_no = ? AND context_manifest_id = ? AND persona_id = ?",
				key.CaseID, key.Round, key.ContextManifestID, key.PersonaID).First(&journal).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return readsetjournal.ErrNotFound
		}
		if err != nil {
			return err
		}
		if journal.ID != journalID {
			return fmt.Errorf("%w: journal ID differs from frozen key", readsetjournal.ErrInvalidJournal)
		}
		if err := readsetjournal.ValidateCurrentScope(scopeFromJournalModel(&journal), current); err != nil {
			return err
		}
		var event ReadSetJournalEventModel
		err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("journal_id = ? AND delivery_id = ?", journal.ID, deliveryID).First(&event).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return readsetjournal.ErrNotFound
		}
		if err != nil {
			return err
		}
		var delivery ReadSetJournalDeliveryModel
		err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("journal_id = ? AND delivery_id = ?", journal.ID, deliveryID).First(&delivery).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return readsetjournal.ErrNotFound
		}
		if err != nil {
			return err
		}
		state := readsetjournal.ProviderState(delivery.State)
		if state == target {
			fact, err = eventFactFromModels(&event, &delivery)
			return err
		}
		allowed := state == readsetjournal.ProviderPending ||
			target == readsetjournal.ProviderUnverifiable && state == readsetjournal.ProviderUnknown
		if !allowed {
			return fmt.Errorf("%w: cannot transition provider state %s to %s", readsetjournal.ErrProviderBlocked, state, target)
		}
		update := tx.Model(&ReadSetJournalDeliveryModel{}).
			Where("journal_id = ? AND delivery_id = ? AND state = ? AND state_version = ?", journal.ID, deliveryID, delivery.State, delivery.StateVersion).
			Updates(map[string]any{
				"state": target, "state_version": delivery.StateVersion + 1,
				"last_execution_generation": owner.ExecutionGeneration,
				"last_owner_job_id":         owner.JobID,
				"last_owner_worker_id":      owner.WorkerID,
				"last_owner_lease_until":    leaseUntil,
				"last_reason":               reason, "updated_at": time.Now().UTC(),
			})
		if update.Error != nil {
			return update.Error
		}
		if update.RowsAffected != 1 {
			return fmt.Errorf("%w: delivery state CAS failed", readsetjournal.ErrProviderBlocked)
		}
		delivery.State = string(target)
		delivery.StateVersion++
		delivery.LastExecutionGeneration = owner.ExecutionGeneration
		delivery.LastOwnerJobID = owner.JobID
		delivery.LastOwnerWorkerID = owner.WorkerID
		delivery.LastOwnerLeaseUntil = leaseUntil
		delivery.LastReason = reason
		fact, err = eventFactFromModels(&event, &delivery)
		return err
	})
	return fact, err
}

// ReconcileDelivery is read-only. Recovery may inspect the original registered
// delivery and its provider state, but must never silently resend it.
func (r *ReadSetJournalRepository) ReconcileDelivery(ctx context.Context, owner *entity.ExecutionContext, key readsetjournal.JournalKey, current readsetjournal.AdmissionScope, deliveryID string) (readsetjournal.EventFact, error) {
	view, err := r.ReadJournal(ctx, owner, key, current)
	if err != nil {
		return readsetjournal.EventFact{}, err
	}
	for _, event := range view.Events {
		if event.DeliveryID == deliveryID {
			return event, nil
		}
	}
	return readsetjournal.EventFact{}, readsetjournal.ErrNotFound
}

func lockOrCreateJournal(tx *gorm.DB, prepared readsetjournal.PreparedRegistration, owner *entity.ExecutionContext, leaseUntil time.Time) (*ReadSetJournalModel, error) {
	var journal ReadSetJournalModel
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("case_id = ? AND round_no = ? AND context_manifest_id = ? AND persona_id = ?",
			prepared.JournalKey.CaseID, prepared.JournalKey.Round, prepared.JournalKey.ContextManifestID, prepared.JournalKey.PersonaID).
		First(&journal).Error
	if err == nil {
		return &journal, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	journal = ReadSetJournalModel{
		ID: prepared.JournalID, CaseID: prepared.JournalKey.CaseID, RoundNo: prepared.JournalKey.Round,
		ContextManifestID: prepared.JournalKey.ContextManifestID, PersonaID: prepared.JournalKey.PersonaID,
		State: string(readsetjournal.JournalOpen), HighWatermark: 0, PendingCount: 0, EventBytes: 0,
		SourceDependencyIndexVersion: prepared.Scope.SourceDependencyIndexVersion,
		AdmissionScopeID:             prepared.Scope.AdmissionScopeID,
		ContextRevocationScopeID:     prepared.Scope.ContextRevocationScopeID,
		ContextRevocationEpoch:       prepared.Scope.ContextRevocationEpoch,
		AuthorityScopeID:             prepared.Scope.AuthorityScopeID, AuthorityEpoch: prepared.Scope.AuthorityEpoch,
		CredentialScopeID: prepared.Scope.CredentialScopeID, CredentialEpoch: prepared.Scope.CredentialEpoch,
		OwnerJobID: owner.JobID, OwnerWorkerID: owner.WorkerID,
		ExecutionGeneration: owner.ExecutionGeneration, OwnerLeaseUntil: leaseUntil,
		StateVersion: 0,
	}
	if err := tx.Create(&journal).Error; err != nil {
		return nil, err
	}
	return &journal, nil
}

func upsertExactNode(tx *gorm.DB, journalID string, prepared readsetjournal.PreparedNode, sequence int64, owner *entity.ExecutionContext, scope readsetjournal.AdmissionScope, now time.Time) error {
	var existing ReadSetJournalNodeModel
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("journal_id = ? AND node_id = ?", journalID, prepared.Node.NodeID).First(&existing).Error
	if err == nil {
		if existing.NodeDigest != prepared.Digest || !bytes.Equal(existing.NodeJSON, prepared.Bytes) ||
			existing.SourceEntryID != prepared.Node.SourceEntryID || existing.SourceVersion != prepared.Node.SourceVersion ||
			existing.Kind != prepared.Node.Kind {
			return fmt.Errorf("%w: registered node descriptor changed", readsetjournal.ErrDependency)
		}
		return nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	var versionOwner int64
	if err := tx.Model(&ReadSetJournalNodeModel{}).
		Where("journal_id = ? AND source_entry_id = ? AND source_version = ?", journalID, prepared.Node.SourceEntryID, prepared.Node.SourceVersion).
		Count(&versionOwner).Error; err != nil {
		return err
	}
	if versionOwner != 0 {
		return fmt.Errorf("%w: source/version already bound to another node", readsetjournal.ErrDependency)
	}
	model := ReadSetJournalNodeModel{
		JournalID: journalID, NodeID: prepared.Node.NodeID,
		SourceEntryID: prepared.Node.SourceEntryID, SourceVersion: prepared.Node.SourceVersion,
		Kind: prepared.Node.Kind, NodeDigest: prepared.Digest, NodeJSON: append([]byte(nil), prepared.Bytes...),
		FirstSequence: sequence, AdmissionGeneration: owner.ExecutionGeneration,
		AdmissionJobID: owner.JobID, AdmissionWorkerID: owner.WorkerID,
		AdmissionScopeID: scope.AdmissionScopeID, CreatedAt: now,
	}
	return tx.Create(&model).Error
}

func deliveryState(tx *gorm.DB, journalID, deliveryID string) (*ReadSetJournalDeliveryModel, error) {
	var delivery ReadSetJournalDeliveryModel
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("journal_id = ? AND delivery_id = ?", journalID, deliveryID).First(&delivery).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("%w: registered event lacks delivery state", readsetjournal.ErrInvalidJournal)
	}
	if err != nil {
		return nil, err
	}
	return &delivery, nil
}

func eventFactFromModels(event *ReadSetJournalEventModel, delivery *ReadSetJournalDeliveryModel) (readsetjournal.EventFact, error) {
	if event == nil || delivery == nil {
		return readsetjournal.EventFact{}, fmt.Errorf("%w: missing event/delivery fact", readsetjournal.ErrInvalidJournal)
	}
	if event.RegistrationState != string(readsetjournal.RegistrationRegistered) {
		return readsetjournal.EventFact{}, fmt.Errorf("%w: invalid registration state %q", readsetjournal.ErrInvalidJournal, event.RegistrationState)
	}
	if event.DeliveryID != delivery.DeliveryID || event.LocalSequence != delivery.EventSequence ||
		event.PayloadDigest != delivery.PayloadDigest || event.ProviderRequestID != delivery.ProviderRequestID {
		return readsetjournal.EventFact{}, fmt.Errorf("%w: event/delivery identity mismatch", readsetjournal.ErrInvalidJournal)
	}
	state := readsetjournal.ProviderState(delivery.State)
	switch state {
	case readsetjournal.ProviderPending, readsetjournal.ProviderApplied, readsetjournal.ProviderFailed, readsetjournal.ProviderUnknown, readsetjournal.ProviderUnverifiable:
	default:
		return readsetjournal.EventFact{}, fmt.Errorf("%w: invalid provider state %q", readsetjournal.ErrInvalidJournal, delivery.State)
	}
	fact := readsetjournal.EventFact{
		Sequence: event.LocalSequence, EventVersion: event.EventVersion,
		DeliveryID: event.DeliveryID, ProviderRequestID: event.ProviderRequestID,
		ExecutionGeneration: event.ExecutionGeneration,
		RegistrationState:   readsetjournal.RegistrationState(event.RegistrationState),
		ProviderState:       state, PayloadBytes: append([]byte(nil), event.PayloadJSON...),
		PayloadDigest: event.PayloadDigest, RegisteredAt: event.RegisteredAt,
	}
	if state != readsetjournal.ProviderApplied {
		if delivery.AuthoritySequence != nil || delivery.VerificationRecordJSON != nil || delivery.InjectedAt != nil || delivery.VerifiedAt != nil {
			return readsetjournal.EventFact{}, fmt.Errorf("%w: unverified provider state carries settlement evidence", readsetjournal.ErrInvalidJournal)
		}
		return fact, nil
	}
	if delivery.AuthoritySequence == nil || *delivery.AuthoritySequence <= 0 || *delivery.AuthoritySequence > canonical.MaxSafeInteger ||
		delivery.VerificationRecordJSON == nil || delivery.InjectedAt == nil || delivery.VerifiedAt == nil {
		return readsetjournal.EventFact{}, readsetjournal.ErrAppliedUnverified
	}
	verificationCanonical, err := canonical.CanonicalizeWithLimits(delivery.VerificationRecordJSON, canonical.Limits{
		MaxBytes: 32768, MaxDepth: 16, MaxMembers: 512, MaxStringBytes: 8192,
	})
	if err != nil || !bytes.Equal(verificationCanonical.Bytes(), delivery.VerificationRecordJSON) {
		return readsetjournal.EventFact{}, fmt.Errorf("%w: verification reference bytes are not canonical", readsetjournal.ErrAppliedUnverified)
	}
	var ref inputartifact.ExternalRef
	if err := json.Unmarshal(delivery.VerificationRecordJSON, &ref); err != nil {
		return readsetjournal.EventFact{}, fmt.Errorf("%w: verification reference decode: %v", readsetjournal.ErrAppliedUnverified, err)
	}
	fact.AuthoritySequence = *delivery.AuthoritySequence
	fact.VerificationRecordRef = ref
	fact.InjectedAt = delivery.InjectedAt.UTC().Format("2006-01-02T15:04:05.000Z")
	return fact, nil
}

func decodeStoredNode(model *ReadSetJournalNodeModel) (inputartifact.SourceNode, error) {
	if model == nil {
		return inputartifact.SourceNode{}, fmt.Errorf("%w: nil stored node", readsetjournal.ErrInvalidJournal)
	}
	sum := sha256.Sum256(model.NodeJSON)
	if hex.EncodeToString(sum[:]) != model.NodeDigest {
		return inputartifact.SourceNode{}, fmt.Errorf("%w: stored node digest mismatch", readsetjournal.ErrInvalidJournal)
	}
	canonicalNode, err := canonical.CanonicalizeWithLimits(model.NodeJSON, canonical.Limits{
		MaxBytes: 32768, MaxDepth: 16, MaxMembers: 512, MaxStringBytes: 8192,
	})
	if err != nil || !bytes.Equal(canonicalNode.Bytes(), model.NodeJSON) {
		return inputartifact.SourceNode{}, fmt.Errorf("%w: stored node bytes are not canonical", readsetjournal.ErrInvalidJournal)
	}
	var node inputartifact.SourceNode
	if err := json.Unmarshal(model.NodeJSON, &node); err != nil {
		return inputartifact.SourceNode{}, fmt.Errorf("%w: decode stored node: %v", readsetjournal.ErrInvalidJournal, err)
	}
	if node.NodeID != model.NodeID || node.SourceEntryID != model.SourceEntryID || node.SourceVersion != model.SourceVersion || node.Kind != model.Kind {
		return inputartifact.SourceNode{}, fmt.Errorf("%w: stored node identity mismatch", readsetjournal.ErrInvalidJournal)
	}
	return node, nil
}

func scopeFromJournalModel(journal *ReadSetJournalModel) readsetjournal.AdmissionScope {
	return readsetjournal.AdmissionScope{
		AdmissionScopeID:             journal.AdmissionScopeID,
		SourceDependencyIndexVersion: journal.SourceDependencyIndexVersion,
		ContextRevocationScopeID:     journal.ContextRevocationScopeID,
		ContextRevocationEpoch:       journal.ContextRevocationEpoch,
		AuthorityScopeID:             journal.AuthorityScopeID, AuthorityEpoch: journal.AuthorityEpoch,
		CredentialScopeID: journal.CredentialScopeID, CredentialEpoch: journal.CredentialEpoch,
	}
}

func adapterOwnerMatches(owner *entity.ExecutionContext, fence readsetjournal.OwnerFence) bool {
	return owner != nil && fence.CaseID == owner.CaseID && fence.JobID == owner.JobID &&
		fence.WorkerID == owner.WorkerID && fence.ExecutionGeneration == owner.ExecutionGeneration
}

func validateJournalReadTarget(owner *entity.ExecutionContext, key readsetjournal.JournalKey) error {
	if owner == nil || !owner.IsDurable() || key.CaseID != owner.CaseID {
		return fmt.Errorf("%w: journal key case differs from durable owner", readsetjournal.ErrOwnerFence)
	}
	if _, err := readsetjournal.JournalIDFor(key); err != nil {
		return err
	}
	return nil
}
