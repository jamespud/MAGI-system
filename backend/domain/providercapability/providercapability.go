// Package providercapability evaluates whether a concrete model Provider can
// safely support an M1 controlled read. It only classifies evidence; it does
// not authenticate evidence, call a Provider, issue a receipt, or grant an
// exposure permit.
package providercapability

import (
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"time"
)

const (
	SchemaVersion = "magi-provider-capability/1"
	M1GateNotRun  = "NOT_RUN"
)

type Status string

const (
	StatusVerified     Status = "VERIFIED"
	StatusUnsupported  Status = "UNSUPPORTED"
	StatusUnverifiable Status = "UNVERIFIABLE"
	StatusNotRun       Status = "NOT_RUN"
)

type Verdict string

const (
	VerdictBlocked    Verdict = "BLOCKED"
	VerdictCapability Verdict = "CAPABILITY_VERIFIED"
)

type CapabilityName string

const (
	CapabilitySessionIdentity     CapabilityName = "session_identity"
	CapabilityEnforcedFence       CapabilityName = "enforced_fence"
	CapabilityRevocationWatermark CapabilityName = "revocation_watermark"
	CapabilityTrustedApplyReceipt CapabilityName = "trusted_apply_receipt"
	CapabilityResponseLoss        CapabilityName = "response_loss_recovery"
	CapabilityProviderRestore     CapabilityName = "provider_restore_isolation"
)

// AllCapabilities is the fixed capability matrix. Order is part of the
// machine-readable report contract.
var AllCapabilities = [...]CapabilityName{
	CapabilitySessionIdentity,
	CapabilityEnforcedFence,
	CapabilityRevocationWatermark,
	CapabilityTrustedApplyReceipt,
	CapabilityResponseLoss,
	CapabilityProviderRestore,
}

type DiscoveryOutcome string

const (
	DiscoveryNotRun    DiscoveryOutcome = "NOT_RUN"
	DiscoverySucceeded DiscoveryOutcome = "SUCCEEDED"
	DiscoveryFailed    DiscoveryOutcome = "FAILED"
)

type Provider struct {
	ID      string `json:"id"`
	BaseURL string `json:"base_url"`
}

type ModelsObservation struct {
	Outcome       DiscoveryOutcome `json:"outcome"`
	HTTPStatus    int              `json:"http_status,omitempty"`
	BodySHA256    string           `json:"body_sha256,omitempty"`
	BodyBytes     int              `json:"body_bytes,omitempty"`
	BodySummary   string           `json:"body_summary,omitempty"`
	ModelIDs      []string         `json:"model_ids,omitempty"`
	ObservedAtUTC string           `json:"observed_at_utc,omitempty"`
	ResponseID    string           `json:"response_id,omitempty"`
	ErrorCode     string           `json:"error_code,omitempty"`
}

// TransportObservation records only transport-level facts. A chat ACK, model
// self-report, or gateway log cannot promote any capability by itself.
type TransportObservation struct {
	HTTPStatus    int    `json:"http_status"`
	BodySHA256    string `json:"body_sha256"`
	ResponseID    string `json:"response_id"`
	RequestID     string `json:"request_id"`
	Generation    string `json:"generation"`
	ObservedAtUTC string `json:"observed_at_utc"`
}

type OriginalEvidence struct {
	RawResponseSHA256 string `json:"raw_response_sha256"`
	RawResponseRef    string `json:"raw_response_ref"`
}

type IndependentVerification struct {
	VerifierID          string          `json:"verifier_id"`
	QueryRequestID      string          `json:"query_request_id"`
	QueryResponseSHA256 string          `json:"query_response_sha256"`
	QueryResponseRef    string          `json:"query_response_ref"`
	QuerySource         string          `json:"query_source"`
	ObservedAtUTC       string          `json:"observed_at_utc"`
	Outcome             EvidenceOutcome `json:"outcome"`
}

type EvidenceOutcome string

const (
	EvidencePassed EvidenceOutcome = "PASS"
)

type Evidence struct {
	Capability    CapabilityName          `json:"capability"`
	ProviderID    string                  `json:"provider_id"`
	ModelID       string                  `json:"model_id"`
	ProjectID     string                  `json:"project_id"`
	SessionID     string                  `json:"session_id"`
	RequestID     string                  `json:"request_id"`
	Generation    string                  `json:"generation"`
	ObservedAtUTC string                  `json:"observed_at_utc"`
	Original      OriginalEvidence        `json:"original"`
	Verification  IndependentVerification `json:"verification"`
	Proof         map[string]string       `json:"proof"`
	Reproduction  []string                `json:"reproduction"`
	Expected      string                  `json:"expected"`
	Observed      string                  `json:"observed"`
}

// UnsupportedEvidence is for affirmative interface-contract evidence. A
// missing endpoint, timeout, or an empty model list is not enough.
type UnsupportedEvidence struct {
	ProviderID        string   `json:"provider_id"`
	InterfaceRef      string   `json:"interface_ref"`
	ObservedAtUTC     string   `json:"observed_at_utc"`
	RawResponseSHA256 string   `json:"raw_response_sha256"`
	RawResponseRef    string   `json:"raw_response_ref"`
	Reason            string   `json:"reason"`
	Reproduction      []string `json:"reproduction"`
}

type Observation struct {
	Provider           Provider
	Models             ModelsObservation
	SelectedModelID    string
	Transport          *TransportObservation
	CapabilityEvidence map[CapabilityName][]Evidence
	Unsupported        map[CapabilityName]UnsupportedEvidence
	AssessedAtUTC      string
}

type Target struct {
	ProviderID    string `json:"provider_id"`
	ModelID       string `json:"model_id"`
	DiscoveryRef  string `json:"discovery_ref"`
	ObservedAtUTC string `json:"observed_at_utc"`
	Selection     string `json:"selection"`
}

type CapabilityResult struct {
	Name        CapabilityName       `json:"name"`
	Status      Status               `json:"status"`
	Reason      string               `json:"reason"`
	Evidence    []Evidence           `json:"evidence,omitempty"`
	Unsupported *UnsupportedEvidence `json:"unsupported_evidence,omitempty"`
}

type Report struct {
	SchemaVersion             string             `json:"schema_version"`
	Issue                     string             `json:"issue"`
	Scope                     string             `json:"scope"`
	AssessedAtUTC             string             `json:"assessed_at_utc,omitempty"`
	Provider                  Provider           `json:"provider"`
	Models                    ModelsObservation  `json:"models"`
	Target                    *Target            `json:"target,omitempty"`
	Verdict                   Verdict            `json:"verdict"`
	M1Gate                    string             `json:"m1_gate"`
	Blockers                  []string           `json:"blockers"`
	Capabilities              []CapabilityResult `json:"capabilities"`
	ProductionExposureEnabled bool               `json:"production_exposure_enabled"`
	ApplyVerifiedEmitted      bool               `json:"apply_verified_emitted"`
	DRCPIssued                bool               `json:"drcp_issued"`
	RemoteModelCallsAttempted int                `json:"remote_model_calls_attempted"`
}

var digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Assess applies fail-closed evidence classification. Evidence authenticity
// remains an independent review obligation; VERIFIED here means the supplied
// immutable evidence passes the complete structural binding contract.
func Assess(observation Observation) Report {
	report := Report{
		SchemaVersion:             SchemaVersion,
		Issue:                     "44",
		Scope:                     "M1-03A",
		AssessedAtUTC:             observation.AssessedAtUTC,
		Provider:                  observation.Provider,
		Models:                    observation.Models,
		Verdict:                   VerdictBlocked,
		M1Gate:                    M1GateNotRun,
		ApplyVerifiedEmitted:      false,
		DRCPIssued:                false,
		ProductionExposureEnabled: false,
	}
	report.Target = detectTarget(observation)
	if report.Target == nil {
		report.Blockers = append(report.Blockers, targetBlockers(observation)...)
		for _, name := range AllCapabilities {
			report.Capabilities = append(report.Capabilities, CapabilityResult{
				Name: name, Status: StatusNotRun, Reason: notRunReason(observation),
			})
		}
		return report
	}

	modelSet := make(map[string]struct{}, len(report.TargetModelIDs()))
	if report.Target != nil {
		modelSet[report.Target.ModelID] = struct{}{}
	}
	for _, name := range AllCapabilities {
		result := assessCapability(name, observation, *report.Target, modelSet)
		report.Capabilities = append(report.Capabilities, result)
		if result.Status != StatusVerified {
			report.Blockers = append(report.Blockers, string(name)+":"+string(result.Status))
		}
	}
	if len(report.Blockers) == 0 {
		report.Verdict = VerdictCapability
	}
	return report
}

func (r Report) TargetModelIDs() []string {
	if r.Target == nil || r.Target.ModelID == "" {
		return nil
	}
	return []string{r.Target.ModelID}
}

func detectTarget(observation Observation) *Target {
	models := observation.Models
	if observation.Provider.ID == "" || observation.Provider.BaseURL == "" {
		return nil
	}
	if models.Outcome != DiscoverySucceeded || models.HTTPStatus < 200 || models.HTTPStatus >= 300 {
		return nil
	}
	if !digestPattern.MatchString(models.BodySHA256) || !validUTCTime(models.ObservedAtUTC) {
		return nil
	}
	ids := uniqueNonEmpty(models.ModelIDs)
	if len(ids) == 0 {
		return nil
	}
	selected := strings.TrimSpace(observation.SelectedModelID)
	if selected == "" {
		return nil
	}
	if !slices.Contains(ids, selected) {
		return nil
	}
	return &Target{
		ProviderID:    observation.Provider.ID,
		ModelID:       selected,
		DiscoveryRef:  "GET /v1/models body:sha256:" + models.BodySHA256,
		ObservedAtUTC: models.ObservedAtUTC,
		Selection:     "EXPLICIT_MODEL_ARGUMENT",
	}
}

func targetBlockers(observation Observation) []string {
	models := observation.Models
	switch {
	case observation.Provider.ID == "" || observation.Provider.BaseURL == "":
		return []string{"PROVIDER_NOT_CONFIGURED"}
	case models.Outcome == DiscoveryNotRun:
		return []string{"MODEL_DISCOVERY_NOT_RUN"}
	case models.Outcome == DiscoveryFailed:
		code := strings.TrimSpace(models.ErrorCode)
		if code == "" {
			code = "MODEL_DISCOVERY_FAILED"
		}
		return []string{code}
	case !digestPattern.MatchString(models.BodySHA256) || !validUTCTime(models.ObservedAtUTC):
		return []string{"MODEL_DISCOVERY_EVIDENCE_UNBOUND"}
	case len(uniqueNonEmpty(models.ModelIDs)) == 0:
		return []string{"NO_VERIFIABLE_MODEL_TARGET"}
	case strings.TrimSpace(observation.SelectedModelID) == "":
		return []string{"MODEL_TARGET_NOT_SELECTED"}
	default:
		return []string{"SELECTED_MODEL_NOT_DISCOVERED"}
	}
}

func notRunReason(observation Observation) string {
	blockers := targetBlockers(observation)
	if len(blockers) == 0 {
		return "NO_VERIFIABLE_MODEL_TARGET"
	}
	return blockers[0]
}

func assessCapability(name CapabilityName, observation Observation, target Target, models map[string]struct{}) CapabilityResult {
	evidence, present := observation.CapabilityEvidence[name]
	unsupported, hasUnsupported := observation.Unsupported[name]
	if hasUnsupported {
		if err := validateUnsupported(name, unsupported, observation.Provider); err != nil {
			return CapabilityResult{Name: name, Status: StatusUnverifiable, Reason: "UNSUPPORTED_EVIDENCE_INVALID"}
		}
		if present && len(evidence) > 0 {
			return CapabilityResult{Name: name, Status: StatusUnverifiable, Reason: "CONFLICTING_CAPABILITY_EVIDENCE"}
		}
		value := unsupported
		return CapabilityResult{Name: name, Status: StatusUnsupported, Reason: "INTERFACE_CONTRACT_CONFIRMS_UNSUPPORTED", Unsupported: &value}
	}
	if present && len(evidence) > 0 {
		validated, reason := validateEvidenceSet(name, evidence, observation.Provider, target, models)
		if reason != "" {
			return CapabilityResult{Name: name, Status: StatusUnverifiable, Reason: reason}
		}
		return CapabilityResult{Name: name, Status: StatusVerified, Reason: "INDEPENDENT_QUERY_AND_CAPABILITY_PROOF_VALIDATED", Evidence: validated}
	}
	if observation.Transport != nil {
		return CapabilityResult{Name: name, Status: StatusUnverifiable, Reason: "TRANSPORT_ACK_HAS_NO_INDEPENDENT_ORIGINAL"}
	}
	return CapabilityResult{Name: name, Status: StatusNotRun, Reason: "CAPABILITY_NOT_TESTED"}
}

func validateEvidenceSet(name CapabilityName, evidence []Evidence, provider Provider, target Target, models map[string]struct{}) ([]Evidence, string) {
	byKey := make(map[string]Evidence)
	out := make([]Evidence, 0, len(evidence))
	for _, item := range evidence {
		if item.Capability != name {
			return nil, "EVIDENCE_CAPABILITY_MISMATCH"
		}
		if err := validateEvidence(item, provider, target, models); err != nil {
			return nil, err.Error()
		}
		key := evidenceKey(item)
		if prior, ok := byKey[key]; ok {
			if !sameEvidence(prior, item) {
				return nil, "CONFLICTING_EVIDENCE_FOR_KEY"
			}
			continue
		}
		byKey[key] = item
		out = append(out, item)
	}
	return out, ""
}

func validateEvidence(item Evidence, provider Provider, target Target, models map[string]struct{}) error {
	if strings.TrimSpace(item.ProviderID) == "" || item.ProviderID != provider.ID {
		return fmt.Errorf("EVIDENCE_PROVIDER_UNBOUND")
	}
	if strings.TrimSpace(item.ModelID) == "" {
		return fmt.Errorf("EVIDENCE_MODEL_UNBOUND")
	}
	if _, ok := models[item.ModelID]; !ok {
		return fmt.Errorf("EVIDENCE_MODEL_UNBOUND")
	}
	if strings.TrimSpace(item.ProjectID) == "" || strings.TrimSpace(item.SessionID) == "" ||
		strings.TrimSpace(item.RequestID) == "" || strings.TrimSpace(item.Generation) == "" {
		return fmt.Errorf("EVIDENCE_IDENTITY_UNBOUND")
	}
	if !validUTCTime(item.ObservedAtUTC) || !validUTCTime(item.Verification.ObservedAtUTC) {
		return fmt.Errorf("EVIDENCE_UTC_INVALID")
	}
	if !digestPattern.MatchString(item.Original.RawResponseSHA256) ||
		!digestPattern.MatchString(item.Verification.QueryResponseSHA256) {
		return fmt.Errorf("EVIDENCE_DIGEST_INVALID")
	}
	if strings.TrimSpace(item.Original.RawResponseRef) == "" ||
		strings.TrimSpace(item.Verification.VerifierID) == "" ||
		strings.TrimSpace(item.Verification.QueryRequestID) == "" ||
		strings.TrimSpace(item.Verification.QueryResponseRef) == "" ||
		strings.TrimSpace(item.Verification.QuerySource) == "" ||
		item.Verification.Outcome != EvidencePassed {
		return fmt.Errorf("EVIDENCE_INDEPENDENT_QUERY_UNBOUND")
	}
	if !validStrings(item.Reproduction) || strings.TrimSpace(item.Expected) == "" || strings.TrimSpace(item.Observed) == "" {
		return fmt.Errorf("EVIDENCE_REPRODUCTION_INCOMPLETE")
	}
	if target.ProviderID != provider.ID || target.ModelID != item.ModelID {
		return fmt.Errorf("EVIDENCE_TARGET_UNBOUND")
	}
	if err := validateProof(item.Capability, item.Proof, item); err != nil {
		return err
	}
	return nil
}

func validateProof(capability CapabilityName, proof map[string]string, evidence Evidence) error {
	exact := func(keys ...string) bool {
		if len(proof) != len(keys) {
			return false
		}
		for _, key := range keys {
			if strings.TrimSpace(proof[key]) == "" {
				return false
			}
		}
		return true
	}
	switch capability {
	case CapabilitySessionIdentity:
		if proof != nil && len(proof) != 0 {
			return fmt.Errorf("EVIDENCE_CAPABILITY_PROOF_INVALID")
		}
	case CapabilityEnforcedFence:
		if !exact("fence_id", "fence_decision") || proof["fence_decision"] != "DENIED_AFTER_REVOCATION" {
			return fmt.Errorf("EVIDENCE_CAPABILITY_PROOF_INVALID")
		}
	case CapabilityRevocationWatermark:
		if !exact("admission_authority_sequence", "revocation_watermark", "decision") || proof["decision"] != "DENIED" ||
			proof["admission_authority_sequence"] >= proof["revocation_watermark"] {
			return fmt.Errorf("EVIDENCE_CAPABILITY_PROOF_INVALID")
		}
	case CapabilityTrustedApplyReceipt:
		if !exact("apply_receipt_ref", "apply_receipt_sha256", "applied_session_id") ||
			!digestPattern.MatchString(proof["apply_receipt_sha256"]) || proof["applied_session_id"] != evidence.SessionID {
			return fmt.Errorf("EVIDENCE_CAPABILITY_PROOF_INVALID")
		}
	case CapabilityResponseLoss:
		if !exact("recovered_response_id", "retry_attempted") || proof["retry_attempted"] != "false" {
			return fmt.Errorf("EVIDENCE_CAPABILITY_PROOF_INVALID")
		}
	case CapabilityProviderRestore:
		if !exact("restore_id", "restore_decision") || proof["restore_decision"] != "DENIED_AFTER_RESTORE" {
			return fmt.Errorf("EVIDENCE_CAPABILITY_PROOF_INVALID")
		}
	default:
		return fmt.Errorf("EVIDENCE_CAPABILITY_UNKNOWN")
	}
	return nil
}

func validateUnsupported(name CapabilityName, item UnsupportedEvidence, provider Provider) error {
	if item.ProviderID == "" || item.ProviderID != provider.ID {
		return fmt.Errorf("UNSUPPORTED_PROVIDER_UNBOUND")
	}
	if strings.TrimSpace(item.InterfaceRef) == "" || strings.TrimSpace(item.Reason) == "" ||
		strings.TrimSpace(item.RawResponseRef) == "" || !validStrings(item.Reproduction) {
		return fmt.Errorf("UNSUPPORTED_EVIDENCE_INCOMPLETE")
	}
	if !validUTCTime(item.ObservedAtUTC) || !digestPattern.MatchString(item.RawResponseSHA256) {
		return fmt.Errorf("UNSUPPORTED_EVIDENCE_UNBOUND")
	}
	if !slices.Contains(AllCapabilities[:], name) {
		return fmt.Errorf("UNSUPPORTED_CAPABILITY_UNKNOWN")
	}
	return nil
}

func sameEvidence(a, b Evidence) bool {
	return reflect.DeepEqual(a, b)
}

func evidenceKey(item Evidence) string {
	return strings.Join([]string{
		item.ProviderID, item.ModelID, item.ProjectID, item.SessionID,
		item.RequestID, item.Generation, string(item.Capability),
	}, "\x00")
}

func validUTCTime(value string) bool {
	if !strings.HasSuffix(value, "Z") {
		return false
	}
	parsed, err := time.Parse(time.RFC3339, value)
	return err == nil && parsed.Location() == time.UTC
}

func validStrings(values []string) bool {
	if len(values) == 0 {
		return false
	}
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			return false
		}
	}
	return true
}

func uniqueNonEmpty(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
