package ai

// TemplateSet is one versioned set of TRUSTED instruction texts. The gateway adds
// them to every request; they are the only instructions a model receives. The
// zero value of Options.Templates selects DefaultTemplates.
type TemplateSet struct {
	Version     string // recorded in every Attempt
	Patch       string // application_patch instructions
	Requirement string // requirement_capture instructions
	Repair      string // lead-in of the repair segment (attempt > 1)
}

// DefaultTemplates returns the shipped ai-templates-v1 set.
func DefaultTemplates() TemplateSet { return TemplateSet{Version: TemplateVersion} }
