package compiler

import "fmt"

type Category string
type Scope string
type ExecutionPolicy string
type Priority string

const (
	CategoryFeature  Category = "feature"
	CategoryBugfix   Category = "bugfix"
	CategoryRefactor Category = "refactor"
	CategoryInfra    Category = "infra"
	CategoryAnalysis Category = "analysis"
)

const (
	ScopeRepoWide     Scope = "repo-wide"
	ScopeModule       Scope = "module"
	ScopeFileSpecific Scope = "file-specific"
)

const (
	PolicyAutonomous  ExecutionPolicy = "autonomous"
	PolicySupervised  ExecutionPolicy = "supervised"
	PolicySafe        ExecutionPolicy = "safe"
)

const (
	PriorityNormal   Priority = "normal"
	PriorityHigh     Priority = "high"
	PriorityCritical Priority = "critical"
)

type Task struct {
	Type            string          `json:"type"`
	Category        Category        `json:"category"`
	Scope           Scope           `json:"scope"`
	Constraints     []string        `json:"constraints"`
	Deliverables    []string        `json:"deliverables"`
	ExecutionPolicy ExecutionPolicy `json:"execution_policy"`
	Priority        Priority        `json:"priority"`
	RawInput        string          `json:"raw_input"`
}

func (t *Task) Validate() error {
	switch t.Category {
	case CategoryFeature, CategoryBugfix, CategoryRefactor, CategoryInfra, CategoryAnalysis:
	default:
		return fmt.Errorf("compiler: invalid category %q", t.Category)
	}
	switch t.Scope {
	case ScopeRepoWide, ScopeModule, ScopeFileSpecific:
	default:
		return fmt.Errorf("compiler: invalid scope %q", t.Scope)
	}
	switch t.ExecutionPolicy {
	case PolicyAutonomous, PolicySupervised, PolicySafe:
	default:
		return fmt.Errorf("compiler: invalid execution_policy %q", t.ExecutionPolicy)
	}
	switch t.Priority {
	case PriorityNormal, PriorityHigh, PriorityCritical:
	default:
		return fmt.Errorf("compiler: invalid priority %q", t.Priority)
	}
	return nil
}
