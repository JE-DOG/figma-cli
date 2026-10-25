// Package target parses Figma links without making network requests.
package target

import (
	"errors"
	"net/url"
	"regexp"
	"strings"
)

type Target struct {
	FileKey string   `json:"fileKey"`
	NodeIDs []string `json:"nodeIds,omitempty"`
}

var keyPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
var nodePattern = regexp.MustCompile(`^I?[0-9]+[:-][0-9]+(?:;I?[0-9]+[:-][0-9]+)*$`)

// Parse accepts a file/branch key or an HTTPS Figma URL. Explicit IDs override
// the link selection. Hyphens in node IDs are Figma's URL representation of ':'.
func Parse(input string, explicitIDs []string) (Target, error) {
	t := Target{}
	input = strings.TrimSpace(input)
	if keyPattern.MatchString(input) {
		t.FileKey = input
	} else {
		u, err := url.Parse(input)
		if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" || (u.Host != "www.figma.com" && u.Host != "figma.com") {
			return t, errors.New("target must be a file key or an HTTPS figma.com file/node URL")
		}
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) < 2 || !keyPattern.MatchString(parts[1]) {
			return t, errors.New("Figma URL is missing a valid file key")
		}
		switch parts[0] {
		case "file", "design", "proto", "board", "slides":
		default:
			return t, errors.New("unsupported Figma URL type; use a file/design/proto/board/slides link")
		}
		t.FileKey = parts[1]
		if len(parts) >= 3 && parts[2] == "branch" {
			if len(parts) < 4 || !keyPattern.MatchString(parts[3]) {
				return t, errors.New("Figma branch URL is missing a valid branch key")
			}
			t.FileKey = parts[3]
		}
		query, err := url.ParseQuery(u.RawQuery)
		if err != nil {
			return t, errors.New("invalid URL query encoding")
		}
		if len(query["node-id"]) > 1 {
			return t, errors.New("URL contains repeated node-id; use repeated --node flags instead")
		}
		if len(explicitIDs) == 0 {
			explicitIDs = query["node-id"]
		}
	}
	seen := map[string]bool{}
	for _, value := range explicitIDs {
		for id := range strings.SplitSeq(value, ",") {
			id = strings.TrimSpace(id)
			if !nodePattern.MatchString(id) {
				return Target{}, errors.New("node ID must use numeric pairs such as 12:34 or I12:34;56:78 (URL hyphens also accepted)")
			}
			id = strings.ReplaceAll(id, "-", ":")
			if !seen[id] {
				t.NodeIDs = append(t.NodeIDs, id)
				seen[id] = true
			}
		}
	}
	if len(t.NodeIDs) > 100 {
		return Target{}, errors.New("at most 100 node IDs are supported per command")
	}
	return t, nil
}
