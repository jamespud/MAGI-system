package readsetjournal

import (
	"crypto/sha256"
	"encoding/hex"
	"github.com/jamespud/magi/backend/domain/canonical"
	"github.com/jamespud/magi/backend/domain/inputartifact"
)

// ReadSetDeliveries adapts durable journal facts into the existing frozen #42
// validator's Deliveries map. Only independently verified provider facts are
// emitted. REGISTERED/PENDING, FAILED, UNKNOWN and UNVERIFIABLE facts are
// excluded, so local registration can never become a claimed model read.
//
// Sources still require independently authenticated context membership and
// authority admission sequences and are therefore not synthesized here. The
// frozen validator continues to report ActualReads/Authority NOT_RUN.
func (v JournalView) ReadSetDeliveries() (map[string]inputartifact.ReadEvent, error) {
	if len(v.Events) > 512 {
		return nil, ErrLimit
	}
	deliveries := make(map[string]inputartifact.ReadEvent)
	for _, fact := range v.Events {
		if fact.ProviderState != ProviderApplied {
			continue
		}
		if fact.AuthoritySequence <= 0 || fact.AuthoritySequence > canonical.MaxSafeInteger ||
			fact.InjectedAt == "" || !validExternalRef(fact.VerificationRecordRef) {
			return nil, ErrAppliedUnverified
		}
		if _, err := parseFrozenTime(fact.InjectedAt); err != nil {
			return nil, ErrAppliedUnverified
		}
		plan, err := DecodeRegistrationEvent(fact.PayloadBytes)
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(fact.PayloadBytes)
		if fact.Sequence <= 0 || fact.Sequence > canonical.MaxSafeInteger ||
			fact.ExecutionGeneration <= 0 || fact.ExecutionGeneration > canonical.MaxSafeInteger ||
			hex.EncodeToString(sum[:]) != fact.PayloadDigest || plan.DeliveryID != fact.DeliveryID ||
			plan.ProviderRequestID != fact.ProviderRequestID || plan.EventVersion != fact.EventVersion ||
			plan.JournalKey != v.JournalKey || plan.AdmissionScope != v.Scope ||
			plan.ExecutionGeneration != fact.ExecutionGeneration ||
			plan.JournalKey.PersonaID != plan.TargetPersona ||
			fact.RegistrationState != RegistrationRegistered {
			return nil, ErrAppliedUnverified
		}
		if _, duplicate := deliveries[fact.DeliveryID]; duplicate {
			return nil, ErrInvalidJournal
		}
		event := inputartifact.ReadEvent{
			JournalKey: v.JournalKey, Sequence: fact.Sequence,
			AuthoritySequence:   fact.AuthoritySequence,
			ExecutionGeneration: fact.ExecutionGeneration,
			DeliveryID:          fact.DeliveryID, Kind: plan.Kind, Phase: plan.Phase,
			Source: plan.Source, ParentNodeIDs: append([]string(nil), plan.ParentNodeIDs...),
			PermissionRef: plan.PermissionRef, Producer: plan.Producer,
			VerificationRecordRef: fact.VerificationRecordRef,
			InjectedAt:            fact.InjectedAt, TargetPersona: plan.TargetPersona,
		}
		deliveries[fact.DeliveryID] = event
	}
	return deliveries, nil
}

func validExternalRef(ref inputartifact.ExternalRef) bool {
	return validToken(ref.Namespace) && validToken(ref.ObjectID) && validFixedVersion(ref.Version) &&
		ref.DigestAlg == "sha256" && digestRE.MatchString(ref.ContentDigest)
}
