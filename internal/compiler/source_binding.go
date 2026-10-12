package compiler

import (
	"bytes"
	"encoding/json"
	"github.com/Karthikeyan49/Framwork/internal/metadata"
	"sort"
)

// MatchesStoredSource checks the V1 normalization contract, without compiling
// again. A future IR major version needs its own normalization implementation.
func MatchesStoredSource(p Program, source metadata.Source) bool {
	if !p.Valid() || p.IR().IRVersion != IRVersion {
		return false
	}
	var doc metadata.Document
	if json.Unmarshal(source.JSON(), &doc) != nil {
		return false
	}
	sort.Slice(doc.Definitions, func(i, j int) bool { return doc.Definitions[i].ID < doc.Definitions[j].ID })
	for i := range doc.Definitions {
		d := &doc.Definitions[i]
		if d.Entity != nil {
			sort.Strings(d.Entity.Fields)
		}
		if d.Field != nil {
			sort.Strings(d.Field.EnumValues)
		}
		if d.Workflow != nil {
			for i := range d.Workflow.States {
				sort.Strings(d.Workflow.States[i].ApprovalRoles)
			}
			sort.Slice(d.Workflow.States, func(i, j int) bool { return d.Workflow.States[i].ID < d.Workflow.States[j].ID })
		}
		if d.Tool != nil {
			sort.Strings(d.Tool.Permissions)
			normalizeValueSchema(&d.Tool.InputSchema)
			normalizeValueSchema(&d.Tool.OutputSchema)
		}
	}
	raw, _ := json.Marshal(doc)
	compiled, _ := json.Marshal(p.IR().Application)
	return bytes.Equal(raw, compiled)
}
