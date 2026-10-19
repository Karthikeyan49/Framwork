package corpus

import (
	"strings"
	"testing"
)

func load(t *testing.T) *Library {
	t.Helper()
	lib, err := Load("../testdata/corpus")
	if err != nil {
		t.Fatal(err)
	}
	return lib
}

func TestEveryItemLoadsAndExpands(t *testing.T) {
	lib := load(t)
	if len(lib.Items) != 412 {
		t.Fatalf("%d items, the corpus has 412", len(lib.Items))
	}
	classes := map[string]int{}
	for _, it := range lib.Items {
		classes[it.Class]++
		c, err := lib.Expand(it)
		if err != nil {
			t.Errorf("%s: %v", it.ID, err)
			continue
		}
		if len(c.Payloads) != len(it.Script) {
			t.Errorf("%s: %d payloads for %d steps", it.ID, len(c.Payloads), len(it.Script))
		}
		if strings.Contains(c.Request, "{{") && !strings.Contains(it.Description, "placeholder") {
			t.Errorf("%s: unexpanded placeholder in the request", it.ID)
		}
		for i, p := range c.Payloads {
			if it.Script[i].Error == "" && it.Script[i].Payload == nil {
				t.Errorf("%s step %d: no payload and no error", it.ID, i)
			}
			if strings.Contains(string(p.Payload), "{{hash") || strings.Contains(string(p.Payload), "$def") || strings.Contains(string(p.Payload), "$gen") {
				t.Errorf("%s step %d: unexpanded construct", it.ID, i)
			}
		}
	}
	want := map[string]int{"direct_injection": 36, "indirect_injection": 30, "exfiltration": 28, "hostile_output": 275, "resource": 27, "golden": 16}
	for k, v := range want {
		if classes[k] != v {
			t.Errorf("class %s has %d items, want %d", k, classes[k], v)
		}
	}
}

func TestPlaceholderLanguage(t *testing.T) {
	lib := load(t)
	c, err := lib.Expand(mustItem(t, lib, "dir-length-4001"))
	if err != nil || len(c.Request) != 4001 {
		t.Fatalf("%d %v", len(c.Request), err)
	}
	c, err = lib.Expand(mustItem(t, lib, "gld-ops-50"))
	if err != nil || strings.Count(string(c.Payloads[0].Payload), `"op":"add"`) != 50 {
		t.Fatalf("generator: %v", err)
	}
	c, err = lib.Expand(mustItem(t, lib, "hos-syn-max-output-exact"))
	if err != nil || len(c.Payloads[0].Payload) != 262144 {
		t.Fatalf("pad_to: %d %v", len(c.Payloads[0].Payload), err)
	}
	c, err = lib.Expand(mustItem(t, lib, "ind-bidi"))
	if err != nil || !strings.Contains(c.Draft.Definitions[0].Label+c.Draft.Definitions[1].Label+c.Draft.Definitions[2].Label+c.Draft.Definitions[3].Label, "‮") {
		var labels []string
		for _, d := range c.Draft.Definitions {
			labels = append(labels, d.Label)
		}
		t.Fatalf("plant: %v %v", labels, err)
	}
}

func mustItem(t *testing.T, lib *Library, id string) Item {
	t.Helper()
	it, ok := lib.ByID(id)
	if !ok {
		t.Fatal("no item", id)
	}
	return it
}
