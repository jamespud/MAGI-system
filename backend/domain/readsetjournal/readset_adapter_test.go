package readsetjournal

import (
	"errors"
	"testing"

	"github.com/jamespud/magi/backend/domain/inputartifact"
)

func TestReadSetDeliveriesNeverPromotesUnverifiedRegistration(t *testing.T) {
	prepared, err := PrepareRegistration(enabledConfig(), validRegisterRequest())
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range []ProviderState{ProviderPending, ProviderUnknown, ProviderUnverifiable, ProviderFailed} {
		t.Run(string(state), func(t *testing.T) {
			view := JournalView{
				JournalKey: prepared.JournalKey,
				Events: []EventFact{{
					Sequence: 1, DeliveryID: prepared.Event.DeliveryID,
					ExecutionGeneration: prepared.Event.ExecutionGeneration,
					RegistrationState:   RegistrationRegistered, ProviderState: state,
					PayloadBytes: prepared.EventBytes, PayloadDigest: prepared.EventDigest,
				}},
			}
			deliveries, err := view.ReadSetDeliveries()
			if err != nil || len(deliveries) != 0 {
				t.Fatalf("state=%s deliveries=%+v err=%v", state, deliveries, err)
			}
		})
	}
}

func TestReadSetDeliveriesRejectsAppliedWithoutTrustedEvidence(t *testing.T) {
	prepared, err := PrepareRegistration(enabledConfig(), validRegisterRequest())
	if err != nil {
		t.Fatal(err)
	}
	view := JournalView{
		JournalKey: prepared.JournalKey,
		Events: []EventFact{{
			Sequence: 1, DeliveryID: prepared.Event.DeliveryID,
			ExecutionGeneration: prepared.Event.ExecutionGeneration,
			RegistrationState:   RegistrationRegistered, ProviderState: ProviderApplied,
			PayloadBytes: prepared.EventBytes, PayloadDigest: prepared.EventDigest,
		}},
	}
	if _, err := view.ReadSetDeliveries(); !errors.Is(err, ErrAppliedUnverified) {
		t.Fatalf("error=%v, want ErrAppliedUnverified", err)
	}
}

func TestReadSetDeliveriesAdaptsExistingAppliedFactShape(t *testing.T) {
	// This is a pure storage-shape test. It does not construct the APPLIED
	// state, authenticate a provider, or establish that a model saw the input.
	prepared, err := PrepareRegistration(enabledConfig(), validRegisterRequest())
	if err != nil {
		t.Fatal(err)
	}
	verificationRef := inputartifact.ExternalRef{
		Namespace: "test", ObjectID: "verified-delivery-1", Version: "1",
		DigestAlg: "sha256", ContentDigest: digestOf('e'),
	}
	view := JournalView{
		JournalKey: prepared.JournalKey,
		Scope:      prepared.Scope,
		Events: []EventFact{{
			Sequence: 1, EventVersion: EventVersion, DeliveryID: prepared.Event.DeliveryID,
			ProviderRequestID:   prepared.Event.ProviderRequestID,
			ExecutionGeneration: prepared.Event.ExecutionGeneration,
			RegistrationState:   RegistrationRegistered, ProviderState: ProviderApplied,
			PayloadBytes: prepared.EventBytes, PayloadDigest: prepared.EventDigest,
			AuthoritySequence: 42, VerificationRecordRef: verificationRef,
			InjectedAt: "2026-10-24T12:00:00.000Z",
		}},
	}
	deliveries, err := view.ReadSetDeliveries()
	if err != nil {
		t.Fatal(err)
	}
	event, ok := deliveries[prepared.Event.DeliveryID]
	if !ok {
		t.Fatal("applied fact missing from validator deliveries")
	}
	if event.JournalKey != prepared.JournalKey || event.Sequence != 1 || event.AuthoritySequence != 42 ||
		event.ExecutionGeneration != prepared.Event.ExecutionGeneration ||
		event.VerificationRecordRef != verificationRef || event.InjectedAt != "2026-10-24T12:00:00.000Z" ||
		event.Source.NodeID != prepared.Event.Source.NodeID || event.TargetPersona != prepared.Event.TargetPersona {
		t.Fatalf("adapted event lost exact bindings: %+v", event)
	}
}
