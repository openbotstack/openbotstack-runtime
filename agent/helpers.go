// Package agent provides the DualLoopAgent that implements the agent.Agent interface
// using the dual bounded loop kernel for iterative reasoning and workflow orchestration.
package agent

import (
	agent "github.com/openbotstack/openbotstack-core/control/agent"
	"github.com/openbotstack/openbotstack-core/planner"
)

// agentSkillToPlannerSkill converts agent.SkillDescriptor to planner.SkillDescriptor.
// Both structs have identical fields (ID, Name, Description, InputSchema) but are
// distinct types in different packages.
func agentSkillToPlannerSkill(descs []agent.SkillDescriptor) []planner.SkillDescriptor {
	if len(descs) == 0 {
		return []planner.SkillDescriptor{}
	}
	result := make([]planner.SkillDescriptor, len(descs))
	for i, d := range descs {
		result[i] = planner.SkillDescriptor{
			ID:          d.ID,
			Name:        d.Name,
			Description: d.Description,
			InputSchema: d.InputSchema,
		}
	}
	return result
}
