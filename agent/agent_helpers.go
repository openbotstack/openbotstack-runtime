package agent

import (
	csSkills "github.com/openbotstack/openbotstack-core/control/skills"
	"github.com/openbotstack/openbotstack-core/registry/skills"
)

// defaultMemoryRetrievalLimit is the default number of memories to retrieve during planning.
const defaultMemoryRetrievalLimit = 5

// skillToDescriptor extracts descriptor fields from a Skill.
func skillToDescriptor(id string, s skills.Skill) csSkills.SkillDescriptor {
	return csSkills.SkillDescriptor{
		ID:          s.ID(),
		Name:        s.Name(),
		Description: s.Description(),
		InputSchema: func() *csSkills.JSONSchema {
			if schema := s.InputSchema(); schema != nil {
				return schema
			}
			return nil
		}(),
	}
}

// skillIDsFromDescriptors extracts skill IDs from descriptors.
func skillIDsFromDescriptors(descs []csSkills.SkillDescriptor) []string {
	ids := make([]string, 0, len(descs))
	for _, d := range descs {
		ids = append(ids, d.ID)
	}
	return ids
}
