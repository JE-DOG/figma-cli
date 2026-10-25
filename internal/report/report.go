// Package report extracts read-only implementation information from Figma file responses.
package report

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"unicode/utf16"
)

// Report retains API property names in style and layout maps. An absent field is
// unknown, not an inferred zero or a layout-engine measurement.
type Report struct {
	Name        string            `json:"name"`
	Units       map[string]string `json:"units"`
	Limitations []string          `json:"limitations"`
	Nodes       []Node            `json:"nodes"`
	Truncated   bool              `json:"truncated"`
	Warnings    []string          `json:"warnings"`
}

type Node struct {
	ID         string         `json:"id"`
	Name       string         `json:"name"`
	Type       string         `json:"type"`
	ParentID   string         `json:"parentId,omitempty"`
	Component  *Component     `json:"component,omitempty"`
	Typography *Typography    `json:"typography,omitempty"`
	Layout     map[string]any `json:"layout,omitempty"`
}

type Component struct {
	ID               string `json:"id"`
	Resolved         bool   `json:"resolved"`
	Name             string `json:"name,omitempty"`
	Key              string `json:"key,omitempty"`
	Remote           *bool  `json:"remote,omitempty"`
	ComponentSetID   string `json:"componentSetId,omitempty"`
	ComponentSetName string `json:"componentSetName,omitempty"`
}

type Typography struct {
	Characters string         `json:"characters"`
	Style      map[string]any `json:"style"`
	Runs       []Run          `json:"runs"`
	IndexUnit  string         `json:"indexUnit"`
}

// Run uses half-open UTF-16 offsets, following Figma Plugin API text indexing.
// The REST documentation describes characters without explicitly defining its
// indexing unit, so UTF-16 is an assumption rather than a REST guarantee.
// Style is the base style merged with this run's partial override.
type Run struct {
	Start      int            `json:"start"`
	End        int            `json:"end"`
	Characters string         `json:"characters"`
	OverrideID int            `json:"overrideId"`
	Style      map[string]any `json:"style"`
}

type root struct {
	node          map[string]any
	components    map[string]any
	componentSets map[string]any
	parentID      string
}

// Limits bound typography expansion independently from report node count.
const (
	MaxTextBytes = 256 * 1024
	MaxTextRuns  = 10_000
)

// Build accepts either GET /files/:key or GET /files/:key/nodes responses.
// selectedIDs, if supplied, must all exist. maxNodes caps report traversal, not
// the REST response size or the server's depth (those are caller concerns).
func Build(data []byte, selectedIDs []string, maxNodes int) (Report, error) {
	if maxNodes <= 0 {
		return Report{}, errors.New("max nodes must be positive")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var body map[string]any
	if err := decoder.Decode(&body); err != nil || body == nil {
		return Report{}, errors.New("invalid Figma JSON response")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return Report{}, errors.New("invalid trailing JSON response data")
	}
	if value, ok := body["err"]; ok && value != nil && value != "" {
		return Report{}, errors.New("Figma returned an error response")
	}
	roots, err := responseRoots(body, selectedIDs)
	if err != nil {
		return Report{}, err
	}
	r := Report{
		Name: text(body["name"]),
		Units: map[string]string{
			"geometry":      "Figma px (canvas coordinates); no dp/sp conversion",
			"spacing":       "Figma px",
			"fontSize":      "Figma px",
			"letterSpacing": "Figma px",
			"rotation":      "raw REST rotation (unit unspecified in REST docs)",
			"lineHeight":    "lineHeightUnit: PIXELS, FONT_SIZE_%, or INTRINSIC_%; original fields retained",
		},
		Limitations: []string{
			"Absent properties are unknown; API defaults are not synthesized.",
			"Bounding boxes are absolute canvas bounds after transforms; size/relativeTransform require geometry=paths.",
			"Padding and gaps are declared layout settings, not measured distances; SPACE_BETWEEN and wrapping may change actual distances.",
			"Root parent IDs are unavailable from node responses; server depth limits may omit descendants.",
			"Component source names require response metadata; detached instances cannot be resolved.",
			"Text run offsets assume UTF-16 indexing following Figma Plugin API; REST docs do not explicitly specify the indexing unit.",
		},
		Nodes: []Node{}, Warnings: []string{},
	}
	// Use an explicit stack: deeply nested documents must not grow the Go stack.
	stack := make([]root, 0, len(roots))
	for i := len(roots) - 1; i >= 0; i-- {
		stack = append(stack, roots[i])
	}
	visited := map[string]bool{}
	remainingRuns := MaxTextRuns
	for len(stack) != 0 {
		current := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		id, typ := text(current.node["id"]), text(current.node["type"])
		if id == "" || typ == "" {
			return Report{}, errors.New("invalid Figma node: missing id or type")
		}
		if visited[id] {
			continue
		}
		if len(r.Nodes) == maxNodes {
			r.Truncated = true
			r.Warnings = append(r.Warnings, fmt.Sprintf("Report stopped at maxNodes=%d; remaining nodes omitted.", maxNodes))
			break
		}
		visited[id] = true
		n := Node{ID: id, Name: text(current.node["name"]), Type: typ, ParentID: current.parentID, Layout: properties(current.node, layoutFields)}
		if typ == "INSTANCE" {
			n.Component = resolveComponent(current.node, current.components, current.componentSets)
			if !n.Component.Resolved {
				r.Warnings = append(r.Warnings, "Instance "+id+": source component metadata unavailable.")
			}
		}
		if typ == "TEXT" || typ == "TEXT_PATH" {
			var err error
			n.Typography, err = typography(current.node, &r.Warnings, &remainingRuns)
			if err != nil {
				return Report{}, err
			}
		}
		r.Nodes = append(r.Nodes, n)
		if childrenValue, exists := current.node["children"]; exists {
			children, valid := childrenValue.([]any)
			if !valid {
				return Report{}, errors.New("invalid Figma node children")
			}
			for i := len(children) - 1; i >= 0; i-- {
				child, valid := children[i].(map[string]any)
				if !valid {
					return Report{}, errors.New("invalid Figma child node")
				}
				stack = append(stack, root{child, current.components, current.componentSets, id})
			}
		}
	}
	return r, nil
}

func responseRoots(body map[string]any, selectedIDs []string) ([]root, error) {
	if nodesValue, exists := body["nodes"]; exists {
		nodes, valid := nodesValue.(map[string]any)
		if !valid {
			return nil, errors.New("invalid Figma nodes response")
		}
		ids := selectedIDs
		if len(ids) == 0 {
			ids = make([]string, 0, len(nodes))
			for id := range nodes {
				ids = append(ids, id)
			}
			sort.Strings(ids)
		}
		roots := make([]root, 0, len(ids))
		seen := map[string]bool{}
		for _, id := range ids {
			if seen[id] {
				continue
			}
			seen[id] = true
			wrapper, valid := nodes[id].(map[string]any)
			if !valid {
				return nil, errors.New("requested node is missing or null in Figma response")
			}
			document, valid := wrapper["document"].(map[string]any)
			if !valid {
				return nil, errors.New("invalid Figma node document")
			}
			roots = append(roots, root{node: document, components: object(wrapper["components"]), componentSets: object(wrapper["componentSets"])})
		}
		return roots, nil
	}
	document, valid := body["document"].(map[string]any)
	if !valid {
		return nil, errors.New("Figma response has neither document nor nodes")
	}
	components, sets := object(body["components"]), object(body["componentSets"])
	if len(selectedIDs) == 0 {
		return []root{{node: document, components: components, componentSets: sets}}, nil
	}
	wanted := make(map[string]bool, len(selectedIDs))
	for _, id := range selectedIDs {
		wanted[id] = true
	}
	found := map[string]root{}
	stack := []root{{node: document, components: components, componentSets: sets}}
	for len(stack) > 0 && len(found) < len(wanted) {
		current := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		id := text(current.node["id"])
		if wanted[id] {
			found[id] = current
		}
		children, _ := current.node["children"].([]any)
		for i := len(children) - 1; i >= 0; i-- {
			if child, valid := children[i].(map[string]any); valid {
				stack = append(stack, root{child, components, sets, id})
			}
		}
	}
	roots := make([]root, 0, len(selectedIDs))
	for _, id := range selectedIDs {
		current, exists := found[id]
		if !exists {
			return nil, errors.New("requested node is missing from Figma document")
		}
		roots = append(roots, current)
	}
	return roots, nil
}

func resolveComponent(node, components, sets map[string]any) *Component {
	id := text(node["componentId"])
	component := &Component{ID: id}
	metadata, valid := components[id].(map[string]any)
	if !valid {
		return component
	}
	component.Name = text(metadata["name"])
	component.Key = text(metadata["key"])
	component.Resolved = component.Name != ""
	if remote, ok := metadata["remote"].(bool); ok {
		component.Remote = &remote
	}
	component.ComponentSetID = text(metadata["componentSetId"])
	if set := object(sets[component.ComponentSetID]); set != nil {
		component.ComponentSetName = text(set["name"])
	}
	return component
}

// layoutFields are raw REST fields. Distances and geometry remain in Figma px;
// enums, constraints, null limits and transforms keep their native schema.
var layoutFields = []string{
	"layoutMode", "layoutWrap", "layoutSizingHorizontal", "layoutSizingVertical",
	"primaryAxisSizingMode", "counterAxisSizingMode", "primaryAxisAlignItems", "counterAxisAlignItems", "counterAxisAlignContent",
	"paddingLeft", "paddingRight", "paddingTop", "paddingBottom", "itemSpacing", "counterAxisSpacing",
	"layoutAlign", "layoutGrow", "layoutPositioning", "itemReverseZIndex", "strokesIncludedInLayout",
	"gridRowCount", "gridColumnCount", "gridRowGap", "gridColumnGap", "gridRowsSizing", "gridColumnsSizing",
	"gridChildHorizontalAlign", "gridChildVerticalAlign", "gridRowSpan", "gridColumnSpan", "gridRowAnchorIndex", "gridColumnAnchorIndex",
	"minWidth", "maxWidth", "minHeight", "maxHeight", "width", "height", "constraints",
	"absoluteBoundingBox", "absoluteRenderBounds", "size", "relativeTransform", "rotation",
	"fillGeometry", "strokeGeometry", "clipsContent", "targetAspectRatio", "cornerRadius", "rectangleCornerRadii",
	"boundVariables", "explicitVariableModes",
}

func properties(node map[string]any, fields []string) map[string]any {
	result := map[string]any{}
	for _, key := range fields {
		if value, exists := node[key]; exists {
			result[key] = value
		}
	}
	return result
}

func text(value any) string {
	result, _ := value.(string)
	return result
}

func object(value any) map[string]any {
	result, _ := value.(map[string]any)
	return result
}

func copyStyle(base map[string]any, override map[string]any) map[string]any {
	result := make(map[string]any, len(base)+len(override))
	for key, value := range base {
		result[key] = value
	}
	for key, value := range override {
		result[key] = value
	}
	return result
}

func typography(node map[string]any, warnings *[]string, remainingRuns *int) (*Typography, error) {
	characters := text(node["characters"])
	if len(characters) > MaxTextBytes {
		return nil, errors.New("text exceeds 256 KiB processing limit; choose a narrower selection or use raw")
	}
	base := copyStyle(object(node["style"]), nil)
	result := &Typography{Characters: characters, Style: base, Runs: []Run{}, IndexUnit: "UTF-16 code units; end exclusive"}
	units := utf16.Encode([]rune(characters))
	ids := make([]int, len(units))
	if value, exists := node["characterStyleOverrides"]; exists {
		overrides, valid := value.([]any)
		if !valid {
			*warnings = append(*warnings, "Text "+text(node["id"])+": invalid characterStyleOverrides; using base style.")
		} else {
			if len(overrides) > len(units) {
				*warnings = append(*warnings, "Text "+text(node["id"])+": characterStyleOverrides exceeds UTF-16 text length; extra entries ignored.")
			}
			invalid := false
			for i, value := range overrides {
				if i >= len(ids) {
					break
				}
				number, ok := value.(json.Number)
				id, err := strconv.Atoi(string(number))
				if !ok || err != nil || id < 0 {
					invalid = true
					continue
				}
				ids[i] = id
			}
			if invalid {
				*warnings = append(*warnings, "Text "+text(node["id"])+": invalid override IDs; affected characters use base style.")
			}
		}
	}
	// A style boundary must not split a surrogate pair. Such a response cannot be
	// faithfully represented as valid Unicode strings; keep the whole code point
	// in the first unit's style and explicitly warn about the malformed boundary.
	splitSurrogate := false
	for i := 1; i < len(units); i++ {
		if units[i-1] >= 0xD800 && units[i-1] <= 0xDBFF && units[i] >= 0xDC00 && units[i] <= 0xDFFF && ids[i-1] != ids[i] {
			ids[i] = ids[i-1]
			splitSurrogate = true
		}
	}
	if splitSurrogate {
		*warnings = append(*warnings, "Text "+text(node["id"])+": override boundary split a UTF-16 surrogate pair; first unit's style used for affected characters.")
	}
	table := object(node["styleOverrideTable"])
	missingWarningEmitted := false
	for start := 0; start < len(units); {
		if *remainingRuns == 0 {
			return nil, errors.New("typography exceeds 10000-run processing limit; choose a narrower selection or use raw")
		}
		id := ids[start]
		end := start + 1
		for end < len(units) && ids[end] == id {
			end++
		}
		var override map[string]any
		if id != 0 {
			override = object(table[strconv.Itoa(id)])
			if override == nil && !missingWarningEmitted {
				missingWarningEmitted = true
				*warnings = append(*warnings, "Text "+text(node["id"])+": one or more overrides missing from styleOverrideTable; base style retained for those runs.")
			}
		}
		result.Runs = append(result.Runs, Run{Start: start, End: end, Characters: string(utf16.Decode(units[start:end])), OverrideID: id, Style: copyStyle(base, override)})
		*remainingRuns -= 1
		start = end
	}
	return result, nil
}
