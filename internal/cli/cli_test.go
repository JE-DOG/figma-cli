package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeAPI struct {
	data      string
	path      string
	query     url.Values
	downloads []string
	ctx       context.Context
	err       error
}

func (f *fakeAPI) Get(ctx context.Context, path string, q url.Values) ([]byte, error) {
	f.ctx, f.path, f.query = ctx, path, q
	return []byte(f.data), f.err
}
func (f *fakeAPI) Download(_ context.Context, source, target string) error {
	f.downloads = append(f.downloads, target)
	return os.WriteFile(target, []byte("fixture asset"), 0600)
}

func execute(t *testing.T, args []string, input, env string, fake *fakeAPI) (string, string, error) {
	t.Helper()
	var out, errOut bytes.Buffer
	err := run(context.Background(), args, strings.NewReader(input), &out, &errOut, func(key string) string {
		if key != "FIGMA_TOKEN" {
			t.Fatalf("unexpected environment lookup %s", key)
		}
		return env
	}, func(token, auth string, timeout time.Duration, retries int) api {
		if token == "" || timeout <= 0 {
			t.Fatal("invalid client arguments")
		}
		return fake
	})
	return out.String(), errOut.String(), err
}

func TestOfflineCommandsNeedNoToken(t *testing.T) {
	for _, args := range [][]string{{"help"}, {"version"}, {"parse", "https://figma.com/design/KEY/X?node-id=1-2"}, {"inspect", "--help"}} {
		if err := Run(context.Background(), args, nil, io.Discard, io.Discard, func(string) string { t.Fatal("offline command read token"); return "" }); err != nil {
			t.Fatal(err)
		}
	}
}

func TestInspectNodeRequest(t *testing.T) {
	fake := &fakeAPI{data: `{"name":"Demo","nodes":{"1:2":{"document":{"id":"1:2","name":"Frame","type":"FRAME","paddingLeft":0,"itemSpacing":8}}}}`}
	out, errOut, err := execute(t, []string{"inspect", "--node", "1-2", "--depth", "2", "--geometry", "--file-version", "42", "KEY"}, "", "test-pat", fake)
	if err != nil {
		t.Fatal(err)
	}
	if fake.path != "/files/KEY/nodes" || fake.query.Get("ids") != "1:2" || fake.query.Get("depth") != "2" || fake.query.Get("geometry") != "paths" || fake.query.Get("version") != "42" {
		t.Fatalf("wrong request %s %v", fake.path, fake.query)
	}
	if !json.Valid([]byte(out)) || !strings.Contains(out, `"paddingLeft": 0`) || !strings.Contains(errOut, "--depth") {
		t.Fatalf("out=%s errOut=%s", out, errOut)
	}
	if _, ok := fake.ctx.Deadline(); !ok {
		t.Fatal("request lacks total deadline")
	}
}

func TestRawPreservesUnknownFieldsAndPrecision(t *testing.T) {
	data := `{"document":{"id":"0:0"},"unknown":9007199254740993,"nodes":{"1:2":null}}`
	fake := &fakeAPI{data: data}
	out, _, err := execute(t, []string{"raw", "--node", "1:2", "KEY"}, "", "fixture-token", fake)
	if err != nil || out != data+"\n" {
		t.Fatalf("%q %v", out, err)
	}
	fake.data = "<html>error</html>"
	out, _, err = execute(t, []string{"raw", "KEY"}, "", "fixture-token", fake)
	if err == nil || out != "" {
		t.Fatal("invalid JSON was printed")
	}
}

func TestValidationBeforeNetwork(t *testing.T) {
	for _, args := range [][]string{
		{"inspect", "--timeout", "0s", "KEY"}, {"inspect", "--retries", "6", "KEY"}, {"inspect", "--depth", "-1", "KEY"},
		{"inspect", "--max-nodes", "0", "KEY"}, {"inspect", "--auth", "wrong", "KEY"}, {"inspect", "KEY", "--node", "1:2"},
		{"export", "KEY"}, {"export", "--node", "1:2", "--scale", "NaN", "KEY"}, {"export", "--node", "1:2", "--scale", "4.01", "KEY"},
		{"export", "--node", "1:2", "--format", "svg", "--scale", "2", "KEY"}, {"export", "--node", "1:2", "--svg-text", "KEY"},
		{"export", "--node", "1:2", "--format", "pdf", "KEY"}, {"inspect", "--token", "never-supported", "KEY"},
	} {
		fake := &fakeAPI{}
		out, _, err := execute(t, args, "", "fixture-token", fake)
		if err == nil || fake.path != "" || out != "" {
			t.Fatalf("validation failed for %v: %v", args, err)
		}
	}
}

func TestTokenSourcesAndRedaction(t *testing.T) {
	var out bytes.Buffer
	const stdinToken = "dummy-stdin-secret"
	err := run(context.Background(), []string{"raw", "--auth", "oauth", "--token-stdin", "KEY"}, strings.NewReader(stdinToken+"\n"), &out, io.Discard, func(string) string { return "ignored-env" }, func(token, auth string, _ time.Duration, _ int) api {
		if token != stdinToken || auth != "oauth" {
			t.Fatal("stdin did not override env")
		}
		return &fakeAPI{err: errors.New("must redact " + stdinToken)}
	})
	if err == nil || strings.Contains(err.Error(), stdinToken) || !strings.Contains(err.Error(), "[REDACTED]") {
		t.Fatalf("unsafe error: %v", err)
	}
	for _, value := range []string{"", "has\nnewline", strings.Repeat("x", 8193)} {
		if _, err := readToken(strings.NewReader(value), func(string) string { return "" }, true); err == nil {
			t.Fatal("accepted malformed stdin token")
		}
	}
}

func TestExportManifestAndRenderFailures(t *testing.T) {
	root := t.TempDir()
	dest := filepath.Join(root, "success")
	fake := &fakeAPI{data: `{"images":{"1:2":"https://cdn.example.test/one?signature=hidden","3:4":"https://cdn.example.test/two"}}`}
	out, _, err := execute(t, []string{"export", "--node", "1:2,3:4", "--format", "svg", "--svg-text", "--output", dest, "KEY"}, "", "fixture-token", fake)
	if err != nil {
		t.Fatal(err)
	}
	if fake.path != "/images/KEY" || fake.query.Get("svg_outline_text") != "false" || len(fake.downloads) != 2 {
		t.Fatalf("wrong export %s %v", fake.path, fake.query)
	}
	if !strings.Contains(out, "1-2.svg") || strings.Contains(out, "signature") || strings.Contains(out, "https:") {
		t.Fatalf("invalid manifest %s", out)
	}
	for _, data := range []string{`{"images":{"1:2":null}}`, `{"images":{}}`, `{"err":"private server error"}`, `null`, `not json`} {
		fake := &fakeAPI{data: data}
		missingDir := filepath.Join(root, "must-not-create")
		out, _, err := execute(t, []string{"export", "--node", "1:2", "--output", missingDir, "KEY"}, "", "fixture-token", fake)
		if err == nil || out != "" || len(fake.downloads) != 0 {
			t.Fatalf("accepted failed render %s", data)
		}
		if _, err := os.Stat(missingDir); !os.IsNotExist(err) {
			t.Fatal("created directory before validating render")
		}
	}
}
