---
name: figma-cli
description: Inspect Figma file or node URLs through the local figma-cli CLI, read original component names, typography and layout, retrieve raw JSON, or export PNG/SVG. Use for read-only REST inspection or explicit figma-cli requests; not for editing Figma or operating Figma Desktop.
---

# Figma CLI

Use the Go `figma-cli` binary from this repository for read-only REST access.
No running Figma Desktop or MCP connection is needed. It writes only local
export files when requested.

## Locate and authenticate

- Prefer the binary path supplied by the user, then `figma-cli` on PATH, then
  `~/projects/figma-cli/bin/figma-cli`. Use its absolute path from other projects.
  Confirm that `help` describes this read-only REST CLI before using another
  executable with the same name; otherwise use the checkout binary explicitly.
- If the binary is missing, locate the repository containing this skill. Resolve
  symlinks first: this file lives at `skills/figma-cli/SKILL.md` inside the repo.
  Build from that repository with `go build -o bin/figma-cli ./cmd/figma-cli`
  (Go 1.26+). Do not change the caller's project or install a global binary.
- Run `figma-cli help` to confirm the installed command interface. Examples below
  use `figma-cli` as shorthand for the resolved executable; follow any active
  workspace rules for shell wrappers.
- Live commands require `FIGMA_TOKEN` in the CLI process environment, with
  `file_content:read` scope and access to the file. Check presence without printing
  its value. For an existing OAuth token, add `--auth oauth`; PAT is the default.
- Alternatively pipe a user-configured secret manager into `--token-stdin`.
  It reads until EOF, up to 8 KiB, and overrides the environment token. Never put
  a token in argv, logs, files, the skill, or chat. Do not invent a secret-manager
  command or search unrelated configuration files for credentials.
- If no token is available, continue offline URL parsing and ask the user to
  configure the environment or a secret-manager pipe before live requests.
  Do not report fixture or offline results as a successful Figma API check.

## Select the smallest useful request

Put **all flags before the final target**. Quote URLs and composite node IDs.
The target is an HTTPS Figma file/node URL or file/branch key. `parse` is offline:

```sh
figma-cli parse 'https://www.figma.com/design/FILE_KEY/Screen?node-id=12-34'
figma-cli inspect --depth 3 'https://www.figma.com/design/FILE_KEY/Screen?node-id=12-34'
figma-cli inspect --node 12:34 --node 56:78 --depth 2 FILE_KEY
figma-cli inspect --geometry --node 12:34 FILE_KEY
figma-cli raw --node 12:34 FILE_KEY
```

`--node` repeats or accepts comma-separated IDs and replaces the URL selection.
URL `12-34` becomes API `12:34`; composite IDs such as `I12:34;56:78` are accepted.
Branch URLs select the branch key. Add `--file-version VERSION_ID` to pin a
version. Prefer the requested node subtree over fetching the entire file.
Use `--geometry` only when local size, transforms or vector paths are needed.
`--max-nodes` caps the report, not the HTTP response; `--depth` reduces the API
subtree. A shallow request cannot establish that omitted descendants are absent.

## Interpret the report

`inspect` emits JSON with a flat `nodes` array and `parentId` links:

- Instance `name` is the layer's current name. `component.name` is the source
  component name from response metadata; `component.componentSetName` is the
  variant set name when available. Check `component.resolved`; never substitute
  a renamed instance's name when source metadata is missing. Detached instances
  cannot be reliably linked back to their source.
- `typography.style` is the base style; `typography.runs[].style` merges partial
  overrides over it. Use runs when fonts, sizes or weights differ within a node.
  Run offsets are half-open UTF-16 units. This is an implementation assumption
  based on Plugin API indexing, not an explicit REST guarantee. Check warnings
  about missing overrides or surrogate boundaries before making precise claims.
- `layout` preserves supplied padding, gaps, sizing, constraints, bounds and
  optional geometry. Missing, zero and null values are distinct. Values are
  Figma pixels, not Android dp/sp. Gaps are declared settings, not measured
  margins; SPACE_BETWEEN, wrapping and transforms can change actual distances.
- Keep `lineHeightUnit` and its associated fields: `PIXELS`, `FONT_SIZE_%` and
  `INTRINSIC_%` differ. Letter spacing is px. Do not invent AUTO or a rotation
  unit that the REST response does not establish.
- Read `units`, `limitations`, `warnings` and `truncated`. A truncated report is
  incomplete. Use a narrower selection or a suitable `--max-nodes` for follow-up.
  Use `raw` for original fields and numeric precision; it also preserves null
  node entries that make `inspect` fail.

## Export assets

Export only the requested nodes into an explicit directory appropriate to the
task. Network inspection does not require exporting assets.

```sh
figma-cli export --node 12:34 --scale 2 --output ./exports FILE_KEY
figma-cli export --node 12:34 --format svg --output ./exports FILE_KEY
figma-cli export --node 12:34 --format svg --svg-text --output ./exports FILE_KEY
```

PNG scale is 0.01–4; SVG requires scale 1. SVG outlines text by default;
`--svg-text` keeps selectable text but rendering depends on fonts. Use
`--absolute-bounds` for full node bounds. The JSON manifest contains local paths,
not signed CDN URLs. Existing files are never overwritten; choose another output
directory on collision. Failed downloads are cleaned up, but earlier completed
files remain. A null image URL means the node could not be rendered.

## Limits and failures

Defaults: `--timeout 30s` for the entire network command, `--retries 2` for HTTP
429, and `--max-nodes 2000` for reports. At most 100 selected IDs, a 64 MiB JSON
response, a 128 MiB asset, 256 KiB UTF-8 text per node, and 10,000 style runs per
report are supported. Text/run limit errors suggest narrowing the selection or
using `raw`. Without `--depth`, the API subtree is not depth-limited.

The CLI respects `Retry-After` and stops if the wait exceeds the remaining
timeout. Do not add an outer immediate retry loop. Report the wait or adjust
timeout when the task permits. For 401/403, check token validity, scope and file
access without exposing the token; for 404, check the file/branch key. Errors
use stderr and exit code 1; stdout is command data, not diagnostic output.

For setup, complete flag descriptions and API references, read the repository's
Russian `README.md` (two directories above this skill after resolving symlinks).
