package target

import (
	"reflect"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		explicit    []string
		key         string
		ids         []string
	}{
		{"key", "FileKEY", nil, "FileKEY", nil},
		{"encoded", "https://www.figma.com/design/FileKEY/Screen?node-id=12%3A34&t=ignored", nil, "FileKEY", []string{"12:34"}},
		{"hyphens", "https://figma.com/file/FileKEY/Screen?node-id=12-34", nil, "FileKEY", []string{"12:34"}},
		{"instance", "https://www.figma.com/design/FileKEY/Screen?node-id=I12-34%3B56-78", nil, "FileKEY", []string{"I12:34;56:78"}},
		{"branch", "https://www.figma.com/design/MainKEY/branch/BranchKEY/Screen?node-id=1-2", nil, "BranchKEY", []string{"1:2"}},
		{"override", "https://www.figma.com/proto/KEY/Screen?node-id=bad", []string{"1:2,3-4", "1-2"}, "KEY", []string{"1:2", "3:4"}},
		{"board", "https://figma.com/board/KEY/Screen", nil, "KEY", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Parse(tc.input, tc.explicit)
			if err != nil || got.FileKey != tc.key || !reflect.DeepEqual(got.NodeIDs, tc.ids) {
				t.Fatalf("got %#v, %v", got, err)
			}
		})
	}
}

func TestRejectUnsafeAndMalformedTargets(t *testing.T) {
	for _, input := range []string{"", "../KEY", "http://figma.com/design/KEY/X", "https://figma.com.evil/design/KEY/X", "https://evil@figma.com/design/KEY/X", "https://figma.com:443/design/KEY/X", "https://figma.com/community/file/KEY", "https://figma.com/design/KEY/branch/", "https://figma.com/design/KEY/X?node-id=1-2&node-id=3-4", "https://figma.com/design/KEY/X?node-id=%ZZ", "https://figma.com/design/KEY/X?node-id=../../../tmp", "https://figma.com/design/KEY/X?node-id="} {
		if _, err := Parse(input, nil); err == nil {
			t.Errorf("accepted %q", input)
		}
	}
	for _, id := range []string{"1:2,", "1:2/../../x", "", "abc", "1:2?x"} {
		if _, err := Parse("KEY", []string{id}); err == nil {
			t.Errorf("accepted ID %q", id)
		}
	}
	ids := make([]string, 101)
	for i := range ids {
		ids[i] = strings.Repeat("1", i+1) + ":2"
	}
	if _, err := Parse("KEY", ids); err == nil {
		t.Fatal("accepted >100 nodes")
	}
}
