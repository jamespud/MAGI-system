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
