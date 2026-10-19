package tools

import "sort"

// Summary is the only view of a tool that leaves the platform toward a model
// provider (ADR040 section 4): identity, side-effect class, approval requirement
// and the names of the declared input and output fields. It deliberately has no
// field for an endpoint, host, path, credential reference, delivery contract,
// sandbox endpoint or simulated output.
type Summary struct {
	Ref        string   `json:"ref"`
	Version    string   `json:"version"`
	SideEffect string   `json:"side_effect"`
	Approval   string   `json:"approval"`
	Input      []string `json:"input"`
	Output     []string `json:"output"`
}

// Summaries lists the tools the tenant may use, sorted by ref then version. A
// tool restricted to other tenants is not listed, so one tenant's context can
// never reveal another tenant's private tool. The result is a copy.
func (c Catalog) Summaries(tenant string) []Summary {
	out := []Summary{}
	for _, d := range c.definitions {
		if !d.AllowsTenant(tenant) {
			continue
		}
		s := Summary{Ref: d.Ref, Version: d.Version, SideEffect: d.SideEffect, Approval: d.Approval, Input: []string{}, Output: []string{}}
		for _, f := range d.Input {
			s.Input = append(s.Input, f.Name)
		}
		for _, f := range d.Output {
			s.Output = append(s.Output, f.Name)
		}
		sort.Strings(s.Input)
		sort.Strings(s.Output)
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Ref != out[j].Ref {
			return out[i].Ref < out[j].Ref
		}
		return out[i].Version < out[j].Version
	})
	return out
}
