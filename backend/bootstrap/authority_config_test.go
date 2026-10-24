package bootstrap

import "testing"

func TestAuthorityCapabilityDisabledByDefault(t *testing.T) {
	cfg := Config{}
	if cfg.Authority.Enabled {
		t.Fatal("C2 authenticated authority must default disabled")
	}
}
