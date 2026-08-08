package toolpolicy_test

import (
	"testing"

	"github.com/jamespud/magi/backend/application/toolpolicy"
)

func TestPolicy_RequiresApprovalBlocksUnlessAutoApproved(t *testing.T) {
	p := toolpolicy.NewPolicy([]string{"code_runner"}, []string{"web_search"})
	if !p.RequiresApproval("code_runner") {
		t.Fatal("code_runner should require approval")
	}
	if p.Allowed("code_runner") {
		t.Fatal("code_runner must be blocked without auto-approval")
	}
	if !p.Allowed("web_search") {
		t.Fatal("auto-approved tool must be allowed")
	}
	if p.RequiresApproval("web_search") || !p.Allowed("calc") {
		t.Fatal("non-gated tools must be allowed")
	}
}

func TestPolicy_NilAllowsAll(t *testing.T) {
	var p *toolpolicy.Policy
	if !p.Allowed("code_runner") {
		t.Fatal("nil policy must allow all")
	}
}
