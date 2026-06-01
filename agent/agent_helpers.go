package agent

import (
	"github.com/openbotstack/openbotstack-core/capability"
	aitypes "github.com/openbotstack/openbotstack-core/ai/types"
	"github.com/openbotstack/openbotstack-core/registry/skills"
)

// defaultMemoryRetrievalLimit is the default number of memories to retrieve during planning.
const defaultMemoryRetrievalLimit = 5

// skillToDescriptor extracts descriptor fields from a Skill using the canonical
// capability.SkillToDescriptor conversion (ADR-019 Capability Plane).
func skillToDescriptor(id string, s skills.Skill) aitypes.SkillDescriptor {
	return aitypes.SkillDescriptor(capability.SkillToDescriptor(s))
}

// skillIDsFromDescriptors extracts skill IDs from descriptors.
func skillIDsFromDescriptors(descs []aitypes.SkillDescriptor) []string {
	ids := make([]string, 0, len(descs))
	for _, d := range descs {
		ids = append(ids, d.ID)
	}
	return ids
}
