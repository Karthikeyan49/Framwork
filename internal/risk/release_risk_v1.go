package risk

// releaseRiskV1 is policy release-risk-v1 as data (M12 design appendix D).
// It must never be edited once published: change a row by adding
// release-risk-v2, so stored candidates keep re-evaluating byte for byte. The
// golden tests pin its hash and document.
func releaseRiskV1() Spec {
	facts := func(names ...string) Trigger { return Trigger{Any: names} }
	tool := func(effects, idem, changes []string) Trigger {
		return Trigger{Tool: &ToolMatch{SideEffect: cloneStrings(effects), Idempotency: cloneStrings(idem), Change: cloneStrings(changes)}}
	}
	decl := func(effects []string, flag string) Trigger {
		return Trigger{Decl: &DeclMatch{SideEffect: cloneStrings(effects), Change: cloneStrings(declChanges), Flag: flag}}
	}
	unresolved := tool(toolEffects, idempotencies, toolChanges)
	unresolved.Tool.Unresolved = true
	var (
		noEffect   = []string{SideNone, SideRead}
		write      = []string{SideWrite}
		critical   = []string{SideFinancial, SideDestructive}
		keyed      = []string{IdemNativeKey, IdemLookupByReference, IdemDedupeWindow}
		unkeyed    = []string{IdemNone}
		activity   = []string{ChangeAdded, ChangeRepinned}
		removal    = []string{ChangeRemoved}
		inherited  = []string{ChangeInherited}
		everything = []string{ChangeAdded, ChangeRemoved, ChangeRepinned}
	)
	s := Spec{
		Version: PolicyV1,
		Rules: []Rule{
			{ID: "S1", Trigger: facts("page_changed", "label_changed"), Level: "LOW"},
			{ID: "S2", Trigger: facts("entity_added"), Level: "MEDIUM"},
			{ID: "S3", Trigger: Trigger{Storage: &StorageMatch{Class: []string{ClassAdditive}, Reason: reasonAddOptional}}, Level: "LOW"},
			{ID: "S4", Trigger: facts("relationship_changed"), Level: "MEDIUM"},
			{ID: "S5", Trigger: facts("rule_changed"), Level: "MEDIUM"},
			{ID: "S6", Trigger: facts("financial_rule_changed"), Level: "HIGH"},
			{ID: "S7", Trigger: facts("workflow_changed"), Level: "MEDIUM"},
			{ID: "S7b", Trigger: facts("trigger_widened"), Level: "MEDIUM"},
			{ID: "S8", Trigger: facts("approval_gate_changed"), Level: "HIGH"},
			{ID: "S9.read", Trigger: tool(noEffect, idempotencies, everything), Level: "MEDIUM"},
			{ID: "S9.write", Trigger: tool(write, keyed, activity), Level: "HIGH"},
			{ID: "S9.write_unkeyed", Trigger: tool(write, unkeyed, activity), Level: "CRITICAL"},
			{ID: "S9.critical_effect", Trigger: tool(critical, idempotencies, everything), Level: "CRITICAL"},
			{ID: "S9.write_removed", Trigger: tool(write, idempotencies, removal), Level: "MEDIUM"},
			{ID: "S9b.read", Trigger: tool(noEffect, idempotencies, inherited), Level: "MEDIUM"},
			{ID: "S9b.write", Trigger: tool(write, keyed, inherited), Level: "HIGH"},
			{ID: "S9b.write_unkeyed", Trigger: tool(write, unkeyed, inherited), Level: "CRITICAL"},
			{ID: "S9b.critical_effect", Trigger: tool(critical, idempotencies, inherited), Level: "CRITICAL"},
			{ID: "S10.read", Trigger: decl([]string{DeclNone, DeclRead}, ""), Level: "MEDIUM"},
			{ID: "S10.write", Trigger: decl([]string{DeclWrite}, ""), Level: "HIGH"},
			{ID: "S10.critical", Trigger: decl([]string{DeclDestructive, DeclFinancial}, ""), Level: "CRITICAL"},
			{ID: "S10.data_write", Trigger: decl(declEffects, flagGainsDataWrite), Level: "HIGH"},
			{ID: "S10.weakened_approval", Trigger: decl(declEffects, flagWeakensApproval), Level: "HIGH"},
			{ID: "S11", Trigger: facts("plan_only_changed"), Level: "MEDIUM"},
			{ID: "S12", Trigger: facts("ir_upgrade"), Level: "MEDIUM"},
			{ID: "S13", Trigger: facts("first_release"), Level: "MEDIUM"},
			{ID: "S14", Trigger: facts("unclassified"), Level: "HIGH"},
			{ID: "BLOCK.unknown_capability", Trigger: facts("unknown_capability"), Level: OutcomeBlocked},
			{ID: "BLOCK.tool_unresolved", Trigger: unresolved, Level: OutcomeBlocked},
			{ID: "BLOCK.author_is_requester", Trigger: Trigger{Any: []string{"acceptance_author_is_requester"}, MinLevel: "HIGH"}, Level: OutcomeBlocked},
		},
		Thresholds: Thresholds{SmallMax: 10000, LargeMax: 100000},
		Separation: []SeparationRule{
			{Level: "LOW", Approvals: 1, Excluded: []string{}},
			{Level: "MEDIUM", Approvals: 1, Excluded: []string{}},
			{Level: "HIGH", Approvals: 1, Excluded: []string{RoleRequester, RoleAcceptanceAuthor}},
			{Level: "CRITICAL", Approvals: 2, Excluded: []string{RoleRequester, RoleAcceptanceAuthor, RoleSandboxFreezer}},
		},
		Rollback: RollbackSpec{
			Order: cloneStrings(rollbacks),
			Rules: []RollbackRule{
				{Class: RollbackDataNotReversible, Trigger: Trigger{Storage: &StorageMatch{Class: []string{ClassAdditive, ClassTransform, ClassBreaking}}}},
				{Class: RollbackExternalEffects, Trigger: tool([]string{SideWrite, SideFinancial, SideDestructive}, idempotencies, toolChanges)},
				{Class: RollbackExternalEffects, Trigger: decl([]string{DeclWrite, DeclDestructive, DeclFinancial}, "")},
			},
		},
	}
	// Storage x volume, one row per class in NONE, SMALL, LARGE, UNVERIFIED
	// order. EQUAL cells are silent; BREAKING always blocks.
	matrix := [4][4]string{
		{"LOW", "LOW", "LOW", "LOW"},
		{"LOW", "LOW", "MEDIUM", "HIGH"},
		{"MEDIUM", "MEDIUM", "HIGH", "CRITICAL"},
		{OutcomeBlocked, OutcomeBlocked, OutcomeBlocked, OutcomeBlocked},
	}
	for ci, class := range classes {
		for vi, volume := range volumes {
			c := Cell{Class: class, Volume: volume, Level: matrix[ci][vi]}
			if class != ClassEqual {
				c.ID = "SV." + class + "." + volume
			}
			s.Matrix = append(s.Matrix, c)
		}
	}
	return s
}
