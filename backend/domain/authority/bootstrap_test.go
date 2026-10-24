package authority_test

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jamespud/magi/backend/domain/authority"
	"github.com/jamespud/magi/backend/domain/canonical"
)

func signBootstrap(t *testing.T, body authority.TrustBootstrap, seed byte) ([]byte, authority.BootstrapTrust) {
	t.Helper()
	canon := canonicalBytes(t, body)
	msg := append([]byte("magi:authority-bootstrap-signature:v1\n"), canon...)
	sig := ed25519.Sign(fixedPrivateKey(seed), msg)
	envelope := authority.BootstrapEnvelope{
		Bootstrap: body,
		KeyID:     "recovery-key", KeyVersion: 1,
		Algorithm: authority.SignatureAlgorithmEd25519,
		Signature: base64.StdEncoding.EncodeToString(sig),
	}
	raw := canonicalBytes(t, envelope)
	trust := authority.BootstrapTrust{
		IdentitySourceID: "identity-source", IdentitySourceVersion: "identity-v1",
		RecoveryKeyID: "recovery-key", RecoveryKeyVersion: 1,
		Algorithm:  authority.SignatureAlgorithmEd25519,
		PublicKey:  base64.StdEncoding.EncodeToString(fixedPrivateKey(seed).Public().(ed25519.PublicKey)),
		TrustedNow: "2026-10-24T09:35:00.000Z",
	}
	return raw, trust
}

func bootstrapFixture(t *testing.T) ([]byte, authority.BootstrapTrust) {
	t.Helper()
	state, _, _, _ := proofFixture(t)
	governance := credentialFor(1, "cred-governance", "governance", "identity-source", "identity-v1", "credential-scope")
	body := authority.TrustBootstrap{
		Version:     authority.BootstrapVersion,
		TrustDomain: "tenant-a", RootIncarnation: "root-1",
		IdentitySourceID: "identity-source", IdentitySourceVersion: "identity-v1",
		GovernanceSubject: governance.Subject,
		RootGrant:         state.RootGrant,
		Credentials:       state.Credentials,
		OwnerPolicies:     state.Policies,
		InitialScopes: []authority.ScopedEpoch{
			{Kind: authority.ScopeAuthority, ScopeID: "authority-scope", Epoch: 3},
			{Kind: authority.ScopeContext, ScopeID: "context-scope", Epoch: 4},
			{Kind: authority.ScopeCredential, ScopeID: "credential-scope", Epoch: 5},
		},
	}
	return signBootstrap(t, body, 9)
}

func TestBootstrapAuthenticatedOutOfBand(t *testing.T) {
	raw, trust := bootstrapFixture(t)
	body, fingerprint, result := authority.AuthenticateBootstrap(raw, trust)
	if result.Outcome != authority.OutcomeValid {
		t.Fatalf("outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
	}
	if len(fingerprint) != 64 {
		t.Fatalf("fingerprint=%q", fingerprint)
	}
	readBack, readFingerprint, result := authority.ReadBootstrap(raw, fingerprint, trust)
	if result.Outcome != authority.OutcomeValid || readFingerprint != fingerprint ||
		readBack.TrustDomain != body.TrustDomain || readBack.RootIncarnation != body.RootIncarnation {
		t.Fatalf("readback=%+v fp=%s result=%+v", readBack, readFingerprint, result)
	}
}

func TestBootstrapFailsClosed(t *testing.T) {
	t.Run("no deployment recovery root", func(t *testing.T) {
		raw, trust := bootstrapFixture(t)
		trust.PublicKey = ""
		_, _, result := authority.AuthenticateBootstrap(raw, trust)
		if result.Outcome != authority.OutcomeUnknown || result.Code != "BOOTSTRAP_TRUST_MISSING" {
			t.Fatalf("outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
		}
	})

	t.Run("wrong recovery signature", func(t *testing.T) {
		raw, trust := bootstrapFixture(t)
		trust.PublicKey = base64.StdEncoding.EncodeToString(fixedPrivateKey(10).Public().(ed25519.PublicKey))
		_, _, result := authority.AuthenticateBootstrap(raw, trust)
		if result.Outcome != authority.OutcomeDenied || result.Code != "BOOTSTRAP_SIGNATURE_INVALID" {
			t.Fatalf("outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
		}
	})

	t.Run("fixed identity source mismatch", func(t *testing.T) {
		raw, trust := bootstrapFixture(t)
		trust.IdentitySourceVersion = "identity-v2"
		_, _, result := authority.AuthenticateBootstrap(raw, trust)
		if result.Outcome != authority.OutcomeDenied || result.Code != "BOOTSTRAP_IDENTITY_SOURCE_MISMATCH" {
			t.Fatalf("outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
		}
	})

	t.Run("ambiguous credential identity", func(t *testing.T) {
		raw, trust := bootstrapFixture(t)
		parsed, _ := canonical.Canonicalize(raw)
		var envelope authority.BootstrapEnvelope
		if err := jsonUnmarshal(parsed.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		envelope.Bootstrap.Credentials = append(envelope.Bootstrap.Credentials, envelope.Bootstrap.Credentials[0])
		raw, trust = signBootstrap(t, envelope.Bootstrap, 9)
		_, _, result := authority.AuthenticateBootstrap(raw, trust)
		if result.Outcome != authority.OutcomeConflict || result.Code != "BOOTSTRAP_CREDENTIAL_AMBIGUOUS" {
			t.Fatalf("outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
		}
	})

	t.Run("downgrade and tampered readback", func(t *testing.T) {
		raw, trust := bootstrapFixture(t)
		_, fingerprint, result := authority.AuthenticateBootstrap(raw, trust)
		if result.Outcome != authority.OutcomeValid {
			t.Fatal(result)
		}
		_, _, result = authority.ReadBootstrap([]byte(" "+string(raw)), fingerprint, trust)
		if result.Outcome != authority.OutcomeDenied || result.Code != "BOOTSTRAP_BYTES_NON_CANONICAL" {
			t.Fatalf("outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
		}
		_, _, result = authority.ReadBootstrap(raw, strings.Repeat("0", 64), trust)
		if result.Outcome != authority.OutcomeDenied || result.Code != "BOOTSTRAP_FINGERPRINT_MISMATCH" {
			t.Fatalf("outcome=%s code=%s detail=%s", result.Outcome, result.Code, result.Detail)
		}
	})
}

func jsonUnmarshal(raw []byte, v any) error {
	return json.Unmarshal(raw, v)
}

func TestBootstrapReadbackDoesNotReviveHistoricalCredentials(t *testing.T) {
	raw, trust := bootstrapFixture(t)
	_, fingerprint, result := authority.AuthenticateBootstrap(raw, trust)
	if result.Outcome != authority.OutcomeValid {
		t.Fatal(result)
	}
	trust.TrustedNow = "2026-10-24T13:00:00.000Z"
	_, readFingerprint, result := authority.ReadBootstrap(raw, fingerprint, trust)
	if result.Outcome != authority.OutcomeValid || readFingerprint != fingerprint {
		t.Fatalf("readback outcome=%s code=%s detail=%s fingerprint=%s", result.Outcome, result.Code, result.Detail, readFingerprint)
	}
}
