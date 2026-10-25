package report

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

func fixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/nodes.json")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestBuildFixtureSourceComponentsTypographyAndLayout(t *testing.T) {
	r, err := Build(fixture(t), []string{"10:1"}, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Nodes) != 5 || r.Truncated {
		t.Fatalf("nodes=%d truncated=%v", len(r.Nodes), r.Truncated)
	}
	if r.Nodes[2].ID != "10:3" || r.Nodes[2].ParentID != "10:2" {
		t.Fatalf("not depth-first with correct parent: %#v", r.Nodes[2])
	}
	component := r.Nodes[1].Component
	if !component.Resolved || component.Name != "Size=Large, State=Default" || component.Name == r.Nodes[1].Name || component.ComponentSetName != "Buttons / Purchase" || component.Remote == nil || !*component.Remote {
		t.Fatalf("bad component resolution: %#v", component)
	}
	if component := r.Nodes[4].Component; component.Resolved || component.ID != "99:1" || component.Name != "" {
		t.Fatalf("unresolved component invented a source name: %#v", component)
	}
	style := r.Nodes[2].Typography
	if style.Characters != "Pay 😀 now" || len(style.Runs) != 3 {
		t.Fatalf("bad typography: %#v", style)
	}
	run := style.Runs[1]
	if run.Start != 4 || run.End != 6 || run.Characters != "😀" || run.Style["fontWeight"] != json.Number("700") || run.Style["fontFamily"] != "Inter" || run.Style["fontPostScriptName"] != "Inter-Bold" || run.Style["italic"] != false {
		t.Fatalf("override/base style or UTF-16 run wrong: %#v", run)
	}
	if style.Runs[2].Start != 6 || style.Runs[2].End != 10 || style.Runs[2].Style["fontWeight"] != json.Number("400") {
		t.Fatalf("omitted trailing override zeros did not use base: %#v", style.Runs[2])
	}
	empty := r.Nodes[3].Typography
	if empty == nil || empty.Characters != "" || empty.Runs == nil || len(empty.Runs) != 0 || empty.Style["lineHeightUnit"] != "INTRINSIC_%" {
		t.Fatalf("empty text or intrinsic line height lost: %#v", empty)
	}
	layout := r.Nodes[0].Layout
	if layout["paddingTop"] != json.Number("0") || layout["minWidth"] != json.Number("0") {
		t.Fatalf("explicit zeros lost: %#v", layout)
	}
	if value, ok := layout["maxWidth"]; !ok || value != nil {
		t.Fatalf("null max width lost: %#v", layout)
	}
	if _, exists := layout["minHeight"]; exists {
		t.Fatal("absent minHeight synthesized")
	}
	if layout["relativeTransform"] == nil || layout["size"] == nil || r.Units["fontSize"] != "Figma px" || len(r.Limitations) == 0 {
		t.Fatalf("geometry/units/limits absent: %#v", r)
	}
	if len(r.Warnings) != 1 || !strings.Contains(r.Warnings[0], "source component metadata unavailable") {
		t.Fatalf("valid short override array warned, or unresolved instance silent: %#v", r.Warnings)
	}
}

func TestMissingRequestedNodes(t *testing.T) {
	for _, id := range []string{"10:6", "999:999"} {
		if _, err := Build(fixture(t), []string{id}, 100); err == nil {
			t.Errorf("expected missing/null node error for %s", id)
		}
	}
}

func TestBuildNodeLimitAndDeduplication(t *testing.T) {
	data := fixture(t)
	r, err := Build(data, []string{"10:1", "10:1"}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Truncated || len(r.Nodes) != 2 || len(r.Warnings) != 1 || !strings.Contains(r.Warnings[0], "maxNodes=2") {
		t.Fatalf("limit not explicit: %#v", r)
	}
	if _, err := Build(data, nil, 0); err == nil {
		t.Fatal("accepted zero maxNodes")
	}
	// Reaching the exact node count is not truncation; duplicate roots do not add nodes.
	r, err = Build(data, []string{"10:1", "10:1"}, 5)
	if err != nil || r.Truncated || len(r.Nodes) != 5 {
		t.Fatalf("exact limit/dedup failed: %#v, %v", r, err)
	}
}

func TestBuildFileAndSelectedSubtree(t *testing.T) {
	data := []byte(`{"name":"File","document":{"id":"0:0","name":"Document","type":"DOCUMENT","children":[{"id":"1:0","name":"Page","type":"CANVAS","children":[{"id":"1:1","name":"Title","type":"TEXT","characters":"Hi","style":{"fontSize":18,"lineHeightUnit":"FONT_SIZE_%","lineHeightPercentFontSize":150}}]}]},"components":{}}`)
	all, err := Build(data, nil, 100)
	if err != nil || len(all.Nodes) != 3 {
		t.Fatalf("file failed: %#v, %v", all, err)
	}
	selected, err := Build(data, []string{"1:1"}, 100)
	if err != nil || len(selected.Nodes) != 1 || selected.Nodes[0].ParentID != "1:0" || selected.Nodes[0].Typography.Style["lineHeightPercentFontSize"] != json.Number("150") {
		t.Fatalf("selection failed: %#v, %v", selected, err)
	}
	if _, err := Build(data, []string{"2:0"}, 100); err == nil {
		t.Fatal("accepted missing file node")
	}
}

func TestDeterministicNodeOrdering(t *testing.T) {
	data := []byte(`{"nodes":{"2:0":{"document":{"id":"2:0","name":"B","type":"FRAME"}},"1:0":{"document":{"id":"1:0","name":"A","type":"FRAME"}}}}`)
	for range 20 {
		r, err := Build(data, nil, 100)
		if err != nil || len(r.Nodes) != 2 || r.Nodes[0].ID != "1:0" || r.Nodes[1].ID != "2:0" {
			t.Fatalf("unstable ordering: %#v, %v", r.Nodes, err)
		}
	}
	r, err := Build(data, []string{"2:0", "1:0"}, 100)
	if err != nil || r.Nodes[0].ID != "2:0" {
		t.Fatalf("requested ordering lost: %#v, %v", r.Nodes, err)
	}
}

func TestTypographyMalformedOverridesWarnWithoutLosingText(t *testing.T) {
	cases := []struct {
		name, characters, overrides, table, warning string
	}{
		{"missing table", "abc", "[1,1]", "{}", "overrides missing"},
		{"invalid ids", "abc", `[0,"bad",-1]`, "{}", "invalid override IDs"},
		{"excess ids", "a", "[0,0]", "{}", "exceeds UTF-16"},
		{"split surrogate", "😀x", "[1,0]", `{"1":{"fontWeight":700}}`, "split a UTF-16 surrogate"},
		{"wrong array", "abc", "{}", "{}", "invalid characterStyleOverrides"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := []byte(`{"document":{"id":"1:1","type":"TEXT","characters":"` + tc.characters + `","style":{"fontSize":16},"characterStyleOverrides":` + tc.overrides + `,"styleOverrideTable":` + tc.table + `}}`)
			r, err := Build(data, nil, 100)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(strings.Join(r.Warnings, " "), tc.warning) {
				t.Fatalf("missing warning %q: %#v", tc.warning, r.Warnings)
			}
			var text strings.Builder
			for _, run := range r.Nodes[0].Typography.Runs {
				text.WriteString(run.Characters)
			}
			if text.String() != tc.characters {
				t.Fatalf("text corrupted: %q", text.String())
			}
		})
	}
}

func TestInvalidResponseShapes(t *testing.T) {
	for _, data := range []string{
		`null`, `[]`, `{}`, `{"document":null}`, `{"nodes":null}`,
		`{"document":{"id":"1:1"}}`, `{"document":{"id":"1:1","type":"FRAME","children":{}}}`,
		`{"document":{"id":"1:1","type":"FRAME","children":[null]}}`,
		`{"err":"server detail must not be reflected"}`, `{"document":{}} {}`,
	} {
		if _, err := Build([]byte(data), nil, 100); err == nil {
			t.Errorf("accepted invalid response: %s", data)
		}
	}
}

func TestRunStylesAreIndependent(t *testing.T) {
	r, err := Build(fixture(t), []string{"10:1"}, 100)
	if err != nil {
		t.Fatal(err)
	}
	typography := r.Nodes[2].Typography
	firstBefore := copyStyle(typography.Runs[0].Style, nil)
	typography.Runs[1].Style["fontFamily"] = "Changed"
	if !reflect.DeepEqual(firstBefore, typography.Runs[0].Style) || typography.Style["fontFamily"] != "Inter" {
		t.Fatal("run merge mutated base or sibling run")
	}
}

func textNodeData(t *testing.T, nodes ...map[string]any) []byte {
	t.Helper()
	if nodes == nil {
		nodes = []map[string]any{}
	}
	data, err := json.Marshal(map[string]any{"document": map[string]any{
		"id": "0:0", "type": "DOCUMENT", "children": nodes,
	}})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func alternatingText(id string, count int) map[string]any {
	ids := make([]int, count)
	for i := range ids {
		ids[i] = i % 2
	}
	return map[string]any{
		"id": id, "type": "TEXT", "characters": strings.Repeat("a", count),
		"characterStyleOverrides": ids, "style": map[string]any{"fontSize": 16},
		"styleOverrideTable": map[string]any{"1": map[string]any{"fontWeight": 700}},
	}
}

func TestTextByteLimitBeforeExpansion(t *testing.T) {
	for _, characters := range []string{
		strings.Repeat("a", MaxTextBytes+1),
		strings.Repeat("😀", MaxTextBytes/4+1),
	} {
		r, err := Build(textNodeData(t, map[string]any{"id": "1:1", "type": "TEXT", "characters": characters}), nil, 100)
		if err == nil || !strings.Contains(err.Error(), "256 KiB") || !strings.Contains(err.Error(), "raw") {
			t.Fatalf("large text did not report processing limit: %v", err)
		}
		if len(r.Nodes) != 0 {
			t.Fatal("limit error returned partial report")
		}
	}
	// The exact byte limit is accepted and remains a single default-style run.
	r, err := Build(textNodeData(t, map[string]any{"id": "1:1", "type": "TEXT", "characters": strings.Repeat("a", MaxTextBytes)}), nil, 100)
	if err != nil || len(r.Nodes[1].Typography.Runs) != 1 {
		t.Fatalf("exact byte limit rejected: %v", err)
	}
}

func TestGlobalRunLimit(t *testing.T) {
	t.Run("single text exceeds limit", func(t *testing.T) {
		r, err := Build(textNodeData(t, alternatingText("1:1", MaxTextRuns+1)), nil, 100)
		if err == nil || !strings.Contains(err.Error(), "10000-run") || !strings.Contains(err.Error(), "raw") || len(r.Nodes) != 0 {
			t.Fatalf("single text limit not enforced: report nodes %d, error %v", len(r.Nodes), err)
		}
	})
	t.Run("runs shared across nodes", func(t *testing.T) {
		r, err := Build(textNodeData(t, alternatingText("1:1", MaxTextRuns/2+1), alternatingText("1:2", MaxTextRuns/2)), nil, 100)
		if err == nil || !strings.Contains(err.Error(), "10000-run") || len(r.Nodes) != 0 {
			t.Fatalf("global limit not enforced: report nodes %d, error %v", len(r.Nodes), err)
		}
	})
	t.Run("plain text also consumes run budget", func(t *testing.T) {
		_, err := Build(textNodeData(t, alternatingText("1:1", MaxTextRuns), map[string]any{"id": "1:2", "type": "TEXT", "characters": "plain"}), nil, 100)
		if err == nil || !strings.Contains(err.Error(), "10000-run") {
			t.Fatalf("plain text did not consume run budget: %v", err)
		}
	})
	t.Run("exact run budget accepted", func(t *testing.T) {
		r, err := Build(textNodeData(t, alternatingText("1:1", MaxTextRuns)), nil, 100)
		if err != nil || len(r.Nodes[1].Typography.Runs) != MaxTextRuns {
			t.Fatalf("exact budget rejected: %v", err)
		}
	})
}

func TestMalformedOverridesProduceBoundedWarnings(t *testing.T) {
	splitNode := map[string]any{
		"id": "1:1", "type": "TEXT", "characters": "😀😀😀",
		"characterStyleOverrides": []int{1, 0, 1, 0, 1, 0},
		"styleOverrideTable":      map[string]any{"1": map[string]any{"fontWeight": 700}},
	}
	r, err := Build(textNodeData(t, splitNode), nil, 100)
	if err != nil || len(r.Warnings) != 1 || !strings.Contains(r.Warnings[0], "split a UTF-16 surrogate") {
		t.Fatalf("surrogate warnings not bounded: %#v, %v", r.Warnings, err)
	}
	missingNode := map[string]any{
		"id": "1:2", "type": "TEXT", "characters": "abcde",
		"characterStyleOverrides": []int{1, 2, 3, 4, 5},
	}
	r, err = Build(textNodeData(t, missingNode), nil, 100)
	if err != nil || len(r.Warnings) != 1 || !strings.Contains(r.Warnings[0], "overrides missing") {
		t.Fatalf("missing-table warnings not bounded: %#v, %v", r.Warnings, err)
	}
}

func TestUnitsAndIndexingAssumptionsExplicit(t *testing.T) {
	r, err := Build(textNodeData(t), nil, 100)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.Units["rotation"], "unspecified in REST docs") {
		t.Fatalf("unsupported rotation unit claimed: %q", r.Units["rotation"])
	}
	if !strings.Contains(strings.Join(r.Limitations, " "), "assume UTF-16") {
		t.Fatal("UTF-16 assumption not explicit")
	}
}
