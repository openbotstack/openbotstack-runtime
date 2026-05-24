package skillutil

import "github.com/openbotstack/openbotstack-core/registry/skills"

// SkillTypeFromID determines skill type based on ADR-007 three categories:
// declarative (LLM-only), llm-assisted (Wasm+LLM), deterministic (pure Wasm).
func SkillTypeFromID(s skills.Skill) string {
	if em, ok := s.(interface{ ExecutionMode() string }); ok {
		switch em.ExecutionMode() {
		case "declarative":
			return "declarative"
		case "wasm":
			for _, p := range s.RequiredPermissions() {
				if p == "llm:generate" {
					return "llm-assisted"
				}
			}
			return "deterministic"
		case "native":
			return "deterministic"
		}
	}
	return "deterministic"
}
