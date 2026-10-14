package a2aapp

import (
	"testing"

	a2a "github.com/a2aproject/a2a-go/v2/a2a"
)

// C0 canonical-encoding protection: the A2A request hash is persisted as the
// submission dedup key. Its preimage construction (`legacy-a2a-request/v1`,
// json.Marshal of the normalized input with NO domain separator) must stay
// byte-identical, otherwise existing submissions stop replaying correctly.
func TestLegacyA2ARequestHashIsByteFrozen(t *testing.T) {
	got, err := (InputParser{MaxMessageBytes: 4096, MaxParts: 4}).Parse(&a2a.SendMessageRequest{Message: &a2a.Message{
		ID: "msg-1", Role: a2a.MessageRoleUser, ContextID: "ctx-1",
		Parts: a2a.ContentParts{a2a.NewTextPart("first"), a2a.NewTextPart("second")},
	}})
	if err != nil {
		t.Fatal(err)
	}
	const want = "82b3ce273e4b78c6e3ebde5e368b4d5e93427d306b515cc1ea1e36d8f81a9268"
	if got.RequestHash != want {
		t.Fatalf("A2A request hash changed: %s (want %s)", got.RequestHash, want)
	}
}
