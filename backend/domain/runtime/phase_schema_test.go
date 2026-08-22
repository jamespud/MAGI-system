package runtime

import "testing"

func TestPhaseExpectedSchema(t *testing.T) {
	sum := []byte(`{"type":"object","title":"summary"}`)
	vote := []byte(`{"type":"object","title":"vote"}`)
	ref := []byte(`{"type":"object","title":"reflection"}`)
	for phase, want := range map[string][]byte{
		"gather":            sum,
		"reconsider_gather": sum,
		"vote":              vote,
		"reflect":           ref,
		"reconsider_reflect": ref,
		"unknown":           nil,
	} {
		if got := phaseExpectedSchema(phase, sum, vote, ref); string(got) != string(want) {
			t.Fatalf("phase=%q got=%q want=%q", phase, got, want)
		}
	}
}
