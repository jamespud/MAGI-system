package inputartifact

import "fmt"

func validateTools(d DomainProfile) error {
	for i, t := range d.ToolScope {
		path := fmt.Sprintf("$.tool_scope[%d]", i)
		if e := fixedVersion(t.Version, path+".version"); e != nil {
			return e
		}
		if t.Version != t.DefinitionRef.Version {
			return invalid("REFERENCE", path+".definition_ref.version", "tool and frozen definition versions differ")
		}
		if t.Mode == "READ" && t.AdapterMode != "READ_ONLY" {
			return invalid("TOOL_POLICY", path, "supported read declarations require READ_ONLY")
		}
		if t.Mode == "WRITE" {
			if t.AdapterMode != "FENCED_IDEMPOTENT" && t.AdapterMode != "IDEMPOTENT_ONLY" {
				return invalid("TOOL_POLICY", path, "unsupported write capability; READ_ONLY and UNSAFE cannot declare writes")
			}
			for _, r := range t.RiskLevels {
				if r == "R0" {
					return invalid("TOOL_POLICY", path+".risk_levels", "R0 cannot permit protected writes")
				}
				if r == "R3" && d.RiskPolicy.R3Execution == "DISABLED" {
					return invalid("TOOL_POLICY", path+".risk_levels", "R3 execution disabled")
				}
			}
		}
	}
	// Declared consistency never proves adapter capability or grants resource
	// access. The definition and adapter policy remain pending qualifications.
	return nil
}
