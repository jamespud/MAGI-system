package magi

import (
	"testing"
)

func TestDockerCodeRunner_Interpreters(t *testing.T) {
	a := &DockerCodeRunnerAdapter{}
	tests := []struct {
		lang string
		want []string
	}{
		{"python", []string{"python3", "-c"}},
		{"python3", []string{"python3", "-c"}},
		{"javascript", []string{"node", "-e"}},
		{"js", []string{"node", "-e"}},
		{"bash", []string{"bash", "-c"}},
		{"sh", []string{"bash", "-c"}},
	}
	for _, tt := range tests {
		got, ok := a.interpreter(tt.lang)
		if !ok {
			t.Errorf("interpreter(%q) not found", tt.lang)
			continue
		}
		if got[0] != tt.want[0] || got[1] != tt.want[1] {
			t.Errorf("interpreter(%q) = %v, want %v", tt.lang, got, tt.want)
		}
	}
	if _, ok := a.interpreter("ruby"); ok {
		t.Error("ruby should not be supported")
	}
}
