package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"example.com/figma-cli/internal/figma"
	"example.com/figma-cli/internal/report"
	"example.com/figma-cli/internal/target"
)

const cliVersion = "0.0.1"

const help = `figma-cli ` + cliVersion + ` — read-only Figma CLI (REST API)

Usage: figma-cli <command> [flags] <file-key-or-figma-url>
Commands:
  parse     Resolve a URL/file/branch key and node IDs offline
  inspect   Print JSON: components, mixed typography, layout and geometry
  raw       Print the complete API JSON response (no report truncation)
  export    Download PNG/SVG for selected nodes; print a local file manifest
  version   Print version

Shared flags (place flags BEFORE the target):
  --node ID           Node selection; repeat or comma-separate; overrides URL
  --token-stdin       Read token from stdin until EOF instead of FIGMA_TOKEN
  --auth pat|oauth    Token header mode (default pat)
  --timeout 30s       Total network/retry/download deadline
  --retries 2         Retries for HTTP 429 (0..5), respecting Retry-After
  --file-version ID   Pin a Figma file version
inspect/raw:
  --depth N           API traversal depth (0 = unlimited; omitted from request)
  --geometry         Request local size, transforms and vector paths
inspect:
  --max-nodes 2000    Report node limit (1..100000), reports truncation
export:
  --format png|svg    Default png
  --scale 1          PNG scale, 0.01..4 (SVG requires default 1)
  --svg-text         SVG text elements instead of default outlined glyphs
  --absolute-bounds  Export full node bounds
  --output exports   Destination directory; existing files are never replaced

Token: personal access token with file_content:read in FIGMA_TOKEN, or
--token-stdin. Never put a token in arguments. No Figma Desktop required.
Limits: API response 64 MiB, each downloaded asset 128 MiB, <=100 selected IDs.
inspect text limits: 256 KiB per text node, 10000 style runs across the report.
Geometry uses Figma pixels, not Android dp/sp. See README for API limitations.
`

type api interface {
	Get(context.Context, string, url.Values) ([]byte, error)
	Download(context.Context, string, string) error
}

type clientFactory func(string, string, time.Duration, int) api

func Run(ctx context.Context, args []string, in io.Reader, out, errOut io.Writer, getenv func(string) string) error {
	return run(ctx, args, in, out, errOut, getenv, func(token, auth string, timeout time.Duration, retries int) api {
		return figma.NewClient(token, auth, timeout, retries)
	})
}

type idsFlag []string

func (s *idsFlag) String() string     { return strings.Join(*s, ",") }
func (s *idsFlag) Set(v string) error { *s = append(*s, v); return nil }

func run(ctx context.Context, args []string, in io.Reader, out, errOut io.Writer, getenv func(string) string, newClient clientFactory) (resultErr error) {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		_, err := io.WriteString(out, help)
		return err
	}
	command := args[0]
	if command == "version" {
		_, err := fmt.Fprintln(out, "figma-cli "+cliVersion)
		return err
	}
	if command != "parse" && command != "inspect" && command != "raw" && command != "export" {
		return errors.New("unknown command; run figma-cli help")
	}
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	// flag's error text may echo arbitrary arguments; emit our own safe error.
	fs.SetOutput(io.Discard)
	var ids idsFlag
	fs.Var(&ids, "node", "node ID")
	var tokenStdin, geometry, svgText, bounds bool
	var auth, version, format, output string
	var timeout time.Duration
	var scale float64 = 1
	var retries, depth, maxNodes int
	if command != "parse" {
		fs.BoolVar(&tokenStdin, "token-stdin", false, "token via stdin")
		fs.StringVar(&auth, "auth", "pat", "pat or oauth")
		fs.DurationVar(&timeout, "timeout", 30*time.Second, "total network deadline")
		fs.IntVar(&retries, "retries", 2, "429 retries")
		fs.StringVar(&version, "file-version", "", "file version")
	}
	if command == "inspect" || command == "raw" {
		fs.IntVar(&depth, "depth", 0, "API depth")
		fs.BoolVar(&geometry, "geometry", false, "vector paths and local geometry")
	}
	if command == "inspect" {
		fs.IntVar(&maxNodes, "max-nodes", 2000, "maximum report nodes")
	}
	if command == "export" {
		fs.StringVar(&format, "format", "png", "png or svg")
		fs.StringVar(&output, "output", "exports", "destination directory")
		fs.Float64Var(&scale, "scale", 1, "PNG scale")
		fs.BoolVar(&svgText, "svg-text", false, "SVG selectable text")
		fs.BoolVar(&bounds, "absolute-bounds", false, "full node bounds")
	}
	if err := fs.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			_, e := io.WriteString(out, help)
			return e
		}
		return errors.New("invalid flags; run figma-cli help (flags go before the target)")
	}
	if fs.NArg() != 1 {
		return errors.New("provide exactly one file key or Figma URL after all flags")
	}
	t, err := target.Parse(fs.Arg(0), ids)
	if err != nil {
		return err
	}
	if command == "parse" {
		return writeJSON(out, t)
	}
	if auth != "pat" && auth != "oauth" {
		return errors.New("--auth must be pat or oauth")
	}
	if timeout <= 0 {
		return errors.New("--timeout must be positive")
	}
	if retries < 0 || retries > 5 {
		return errors.New("--retries must be between 0 and 5")
	}
	if depth < 0 || depth > 1000 {
		return errors.New("--depth must be between 0 and 1000")
	}
	if command == "inspect" && (maxNodes < 1 || maxNodes > 100000) {
		return errors.New("--max-nodes must be between 1 and 100000")
	}
	if command == "export" {
		if len(t.NodeIDs) == 0 {
			return errors.New("export requires a node URL or --node")
		}
		if format != "png" && format != "svg" {
			return errors.New("--format must be png or svg")
		}
		if math.IsNaN(scale) || math.IsInf(scale, 0) || scale < 0.01 || scale > 4 {
			return errors.New("--scale must be between 0.01 and 4")
		}
		if format == "svg" && scale != 1 {
			return errors.New("--scale applies to PNG only; SVG requires scale 1")
		}
		if format != "svg" && svgText {
			return errors.New("--svg-text requires --format svg")
		}
		if strings.TrimSpace(output) == "" {
			return errors.New("--output must be a directory path")
		}
	}
	token, err := readToken(in, getenv, tokenStdin)
	if err != nil {
		return err
	}
	defer func() {
		if resultErr != nil {
			resultErr = errors.New(strings.ReplaceAll(resultErr.Error(), token, "[REDACTED]"))
		}
	}()
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	client := newClient(token, auth, timeout, retries)
	q := url.Values{}
	if version != "" {
		q.Set("version", version)
	}
	if len(t.NodeIDs) != 0 {
		q.Set("ids", strings.Join(t.NodeIDs, ","))
	}
	if command == "export" {
		q.Set("format", format)
		if format == "png" {
			q.Set("scale", strconv.FormatFloat(scale, 'f', -1, 64))
		}
		if format == "svg" {
			q.Set("svg_outline_text", strconv.FormatBool(!svgText))
		}
		q.Set("use_absolute_bounds", strconv.FormatBool(bounds))
		return export(ctx, client, t, q, output, format, out)
	}
	path := "/files/" + t.FileKey
	if len(t.NodeIDs) != 0 {
		path += "/nodes"
	}
	if depth != 0 {
		q.Set("depth", strconv.Itoa(depth))
	}
	if geometry {
		q.Set("geometry", "paths")
	}
	data, err := client.Get(ctx, path, q)
	if err != nil {
		return err
	}
	if !json.Valid(data) {
		return errors.New("Figma returned invalid JSON")
	}
	if command == "raw" {
		// Preserve all fields and numeric precision, including null node entries.
		_, err = out.Write(data)
		if err == nil {
			_, err = io.WriteString(out, "\n")
		}
		return err
	}
	r, err := report.Build(data, t.NodeIDs, maxNodes)
	if err != nil {
		return err
	}
	if depth != 0 {
		fmt.Fprintln(errOut, "Note: --depth limits the API subtree; absent descendants are not inspected.")
	}
	return writeJSON(out, r)
}

func readToken(in io.Reader, getenv func(string) string, stdin bool) (string, error) {
	value := getenv("FIGMA_TOKEN")
	if stdin {
		data, err := io.ReadAll(io.LimitReader(in, 8193))
		if err != nil {
			return "", errors.New("could not read token from stdin")
		}
		if len(data) > 8192 {
			return "", errors.New("token input exceeds 8 KiB")
		}
		value = string(data)
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", errors.New("set FIGMA_TOKEN or use --token-stdin with a file_content:read token")
	}
	if len(value) > 8192 {
		return "", errors.New("token exceeds 8 KiB")
	}
	for _, c := range value {
		if c < 33 || c > 126 {
			return "", errors.New("token must contain only printable ASCII without whitespace")
		}
	}
	return value, nil
}

func writeJSON(out io.Writer, value any) error {
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	return encoder.Encode(value)
}

func export(ctx context.Context, client api, t target.Target, q url.Values, output, format string, out io.Writer) error {
	data, err := client.Get(ctx, "/images/"+t.FileKey, q)
	if err != nil {
		return err
	}
	var response struct {
		Err    json.RawMessage    `json:"err"`
		Images map[string]*string `json:"images"`
	}
	if json.Unmarshal(data, &response) != nil {
		return errors.New("Figma returned invalid image JSON")
	}
	if len(response.Err) > 0 && string(response.Err) != "null" && string(response.Err) != `""` {
		return errors.New("Figma image rendering failed")
	}
	// Check every result before writing any files; a null means render failure.
	for _, id := range t.NodeIDs {
		if response.Images[id] == nil || *response.Images[id] == "" {
			return fmt.Errorf("Figma could not render node %s (missing or null image)", id)
		}
	}
	if err := os.MkdirAll(output, 0700); err != nil {
		return errors.New("could not create export directory")
	}
	type asset struct {
		NodeID string `json:"nodeId"`
		Path   string `json:"path"`
	}
	assets := make([]asset, 0, len(t.NodeIDs))
	for _, id := range t.NodeIDs {
		filename := strings.NewReplacer(":", "-", ";", "_").Replace(id) + "." + format
		path := filepath.Join(output, filename)
		if err := client.Download(ctx, *response.Images[id], path); err != nil {
			return fmt.Errorf("export node %s: %w; earlier completed exports, if any, remain", id, err)
		}
		assets = append(assets, asset{NodeID: id, Path: path})
	}
	return writeJSON(out, struct {
		Files []asset `json:"files"`
	}{assets})
}
