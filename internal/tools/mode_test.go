package tools

import "testing"

func TestEffectiveModeIsMostRestrictiveHonorableMode(t *testing.T) {
	write := Definition{SideEffect: "WRITE"}
	read := Definition{SideEffect: "READ"}
	cases := []struct {
		name            string
		d               Definition
		declared        string
		ceiling, expect string
	}{
		{"preview keeps declared", write, ModeTestAccount, "", ModeTestAccount},
		{"equal", write, ModeReroute, ModeReroute, ModeReroute},
		{"block ceiling", write, ModeTestAccount, ModeBlock, ModeBlock},
		{"simulate ceiling without fixture", read, ModeReroute, ModeSimulate, ModeBlock},
		{"declared simulate under looser ceiling", read, ModeSimulate, ModeTestAccount, ModeSimulate},
		{"read only ceiling on write", write, ModeReroute, ModeReadOnly, ModeBlock},
		{"read only ceiling on read", read, ModeTestAccount, ModeReadOnly, ModeReadOnly},
		{"reroute ceiling narrows test account", write, ModeTestAccount, ModeReroute, ModeReroute},
		{"declared stricter than ceiling", write, ModeBlock, ModeTestAccount, ModeBlock},
		{"unknown ceiling", write, ModeReroute, "LIVE", ModeBlock},
		{"unknown declared", write, "LIVE", "", ModeBlock},
	}
	for _, c := range cases {
		d := c.d
		d.Sandbox.Mode = c.declared
		got := EffectiveMode(d, c.ceiling)
		if got != c.expect {
			t.Fatalf("%s: %s", c.name, got)
		}
		if got == ModeLive {
			t.Fatalf("%s: exceeded ceiling", c.name)
		}
	}
}

func TestRankMatchesDatabaseOrder(t *testing.T) {
	order := []string{ModeBlock, ModeSimulate, ModeReadOnly, ModeReroute, ModeTestAccount}
	for i, m := range order {
		if Rank(m) != i {
			t.Fatal(m)
		}
	}
	if Rank(ModeLive) < 99 || Rank("") < 99 {
		t.Fatal("unknown modes must rank above every sandbox ceiling")
	}
}
