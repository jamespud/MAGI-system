package toolpolicy

// Policy is the deterministic tool-approval gate. Tools that require approval
// may not execute in autonomous runs unless an admin listed them in
// auto-approved. A nil policy (not configured) allows every tool, preserving
// existing behavior for tests and minimal setups.
type Policy struct {
	requireApproval map[string]bool
	autoApproved    map[string]bool
}

func NewPolicy(requireApproval, autoApproved []string) *Policy {
	p := &Policy{requireApproval: map[string]bool{}, autoApproved: map[string]bool{}}
	for _, name := range requireApproval {
		if name != "" {
			p.requireApproval[name] = true
		}
	}
	for _, name := range autoApproved {
		if name != "" {
			p.autoApproved[name] = true
		}
	}
	return p
}

// RequiresApproval reports whether a tool is gated by the policy.
func (p *Policy) RequiresApproval(toolName string) bool {
	return p != nil && p.requireApproval[toolName]
}

// Allowed reports whether an autonomous run may execute the tool.
func (p *Policy) Allowed(toolName string) bool {
	if p == nil {
		return true
	}
	if !p.requireApproval[toolName] {
		return true
	}
	return p.autoApproved[toolName]
}
