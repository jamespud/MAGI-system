package entity

import "testing"

func TestArtifactRemap_RemapListAndText(t *testing.T) {
	m := NewArtifactRemap()
	m.AddEvidence("EV-001", "case-abc-melchior-r1-investigate-EV-001")
	m.AddEvidence("EV-002", "case-abc-melchior-r1-investigate-EV-002")
	m.AddClaim("CL-001", "case-abc-balthasar-r1-investigate-CL-001")

	got := m.RemapList([]string{"EV-001", "CL-001", "EV-999"})
	if len(got) != 3 || got[0] != "case-abc-melchior-r1-investigate-EV-001" ||
		got[1] != "case-abc-balthasar-r1-investigate-CL-001" || got[2] != "EV-999" {
		t.Fatalf("RemapList: %v", got)
	}

	text := m.RemapText("cite EV-001 and EV-002; claim CL-001; keep EV-000")
	want := "cite case-abc-melchior-r1-investigate-EV-001 and case-abc-melchior-r1-investigate-EV-002; claim case-abc-balthasar-r1-investigate-CL-001; keep EV-000"
	if text != want {
		t.Fatalf("RemapText:\n got %s\nwant %s", text, want)
	}
}

// A namespaced ID contains its bare form as a suffix, so RemapText must be
// idempotent: re-running it (or seeing an already-remapped citation alongside a
// bare one) must not produce a doubled prefix.
func TestArtifactRemap_RemapTextIsIdempotent(t *testing.T) {
	m := NewArtifactRemap()
	m.AddEvidence("EV-001", "case-abc-melchior-r1-investigate-EV-001")
	namespaced := "case-abc-melchior-r1-investigate-EV-001"
	oncemapped := m.RemapText(namespaced)
	if oncemapped != namespaced {
		t.Fatalf("already-namespaced text was rewritten: %s", oncemapped)
	}
	mixed := m.RemapText("see " + namespaced + " and EV-001")
	want := "see " + namespaced + " and " + namespaced
	if mixed != want {
		t.Fatalf("mixed RemapText:\n got %s\nwant %s", mixed, want)
	}
	if twice := m.RemapText(mixed); twice != mixed {
		t.Fatalf("second RemapText changed text: %s", twice)
	}
}

func TestArtifactRemap_Merge(t *testing.T) {
	a := NewArtifactRemap()
	a.AddEvidence("EV-001", "p1-EV-001")
	b := NewArtifactRemap()
	b.AddEvidence("EV-002", "p2-EV-002")
	b.AddClaim("CL-001", "p2-CL-001")
	a.Merge(b)
	if a.lookup("EV-001") != "p1-EV-001" || a.lookup("EV-002") != "p2-EV-002" || a.lookup("CL-001") != "p2-CL-001" {
		t.Fatalf("merge failed: %+v", a)
	}
}

func TestCodeOfRun(t *testing.T) {
	cases := []struct {
		runID string
		want  MagiCode
	}{
		{"case-abc-melchior-r1-investigate", MagiCodeMelchior},
		{"case-abc-balthasar-a2-r1-reconsider", MagiCodeBalthasar},
		{"case-abc-balthasar-g7-r1-reconsider", MagiCodeBalthasar},
		{"case-abc-casper-r2-investigate", MagiCodeCasper},
		{"", ""},
		{"case-abc", ""},
	}
	for _, c := range cases {
		if got := CodeOfRun(c.runID); got != c.want {
			t.Fatalf("CodeOfRun(%q) = %q, want %q", c.runID, got, c.want)
		}
	}
}

// The shield tokens used during rewriting must not collide with characters
// that already appear in the report body: a model can legitimately return
// control characters as JSON escapes, and rewriting them would corrupt text.
func TestArtifactRemap_RemapTextDoesNotRewriteUnrelatedControlChars(t *testing.T) {
	m := NewArtifactRemap()
	m.AddEvidence("EV-001", "case-r1-EV-001")
	in := "\x000\x00 case-r1-EV-001"
	if got := m.RemapText(in); got != in {
		t.Fatalf("control characters were rewritten:\n got %q\nwant %q", got, in)
	}
}

// Overlapping bare IDs must be replaced longest-first and with token
// boundaries: "EV-001" must never partially rewrite "EV-0012".
func TestArtifactRemap_RemapTextHandlesOverlappingIDs(t *testing.T) {
	m := NewArtifactRemap()
	m.AddEvidence("EV-001", "p-EV-001")
	m.AddEvidence("EV-0012", "p-EV-0012")
	in := "see EV-0012 then EV-001 and EV-001"
	want := "see p-EV-0012 then p-EV-001 and p-EV-001"
	if got := m.RemapText(in); got != want {
		t.Fatalf("overlapping rewrite:\n got %s\nwant %s", got, want)
	}
	if twice := m.RemapText(m.RemapText(in)); twice != want {
		t.Fatalf("not idempotent:\n got %s\nwant %s", twice, want)
	}
}

// A bare ID embedded in a longer identifier (e.g. an unknown namespaced id)
// must be left alone rather than partially rewritten.
func TestArtifactRemap_RemapTextRespectsTokenBoundaries(t *testing.T) {
	m := NewArtifactRemap()
	m.AddEvidence("EV-001", "p-EV-001")
	in := "other-scope-EV-001 and EV-001"
	want := "other-scope-EV-001 and p-EV-001"
	if got := m.RemapText(in); got != want {
		t.Fatalf("boundary rewrite:\n got %s\nwant %s", got, want)
	}
}
