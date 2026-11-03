package policy

import "testing"

func TestCompileVersionRefusesInactiveHeadsAndForgedBootstrapVersions(t *testing.T) {
	grants := `"grants":[{"effect":"ALLOW","actions":["releases.approve"],"resource":{"app_id":"*","entity_id":"*","field_id":"*"}}]`
	version := func(name, body string, active, bootstrap bool) RoleVersion {
		d, err := CompileDefinition([]byte(body), "")
		if err != nil {
			t.Fatal(name, err)
		}
		return RoleVersion{Name: name, Version: 1, Hash: d.Hash(), Active: active, Bootstrap: bootstrap, Definition: []byte(body)}
	}
	custom := `{"schema_version":"tenant-capabilities-v1","name":"release_approver",` + grants + `}`
	if _, err := compileVersion(version("release_approver", custom, true, false)); err != nil {
		t.Fatal("an active custom role was refused:", err)
	}
	if _, err := compileVersion(version("release_approver", custom, false, false)); err == nil {
		t.Fatal("an inactive head compiled")
	}
	if _, err := compileVersion(version("release_approver", custom, true, true)); err == nil {
		t.Fatal("a custom name was accepted as a bootstrap version")
	}
	bootstrap := `{"schema_version":"tenant-capabilities-v1","name":"admin","grants":[]}`
	if d, err := compileVersion(version("admin", bootstrap, true, true)); err != nil || !d.legacy {
		t.Fatalf("the bootstrap admin: %v legacy=%v", err, d.legacy)
	}
	if _, err := compileVersion(version("admin", `{"schema_version":"tenant-capabilities-v1","name":"admin",`+grants+`}`, true, true)); err == nil {
		t.Fatal("a bootstrap version with grants compiled")
	}
	forged := version("admin", bootstrap, true, true)
	forged.Hash = version("release_approver", custom, true, false).Hash
	if _, err := compileVersion(forged); err == nil {
		t.Fatal("a body that does not hash to the stored hash compiled")
	}
}
