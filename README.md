# Figma CLI

This is a **vibecoded CLI**, developed with an AI agent.

A small, **read-only** CLI written in Go. It reads a Figma file or subtree,
reports the original components behind instances, typography, layout, and geometry,
and exports PNG/SVG files. It uses the REST API and requires no running Figma
Desktop app, plugin, or browser.

The repository, binary, and skill are all named **`figma-cli`**.

## Install a prebuilt binary

Download a ready-to-run binary from
[GitHub Releases](https://github.com/JE-DOG/figma-cli/releases/latest).
No Go installation or source checkout is required.

| System | Architecture | Download (0.0.1) |
| --- | --- | --- |
| macOS | Intel (amd64) | [tar.gz](https://github.com/JE-DOG/figma-cli/releases/download/v0.0.1/figma-cli_0.0.1_darwin_amd64.tar.gz) |
| macOS | Apple Silicon (arm64) | [tar.gz](https://github.com/JE-DOG/figma-cli/releases/download/v0.0.1/figma-cli_0.0.1_darwin_arm64.tar.gz) |
| Linux | x86-64 (amd64) | [tar.gz](https://github.com/JE-DOG/figma-cli/releases/download/v0.0.1/figma-cli_0.0.1_linux_amd64.tar.gz) |
| Linux | ARM64 | [tar.gz](https://github.com/JE-DOG/figma-cli/releases/download/v0.0.1/figma-cli_0.0.1_linux_arm64.tar.gz) |
| Windows | x86-64 (amd64) | [zip](https://github.com/JE-DOG/figma-cli/releases/download/v0.0.1/figma-cli_0.0.1_windows_amd64.zip) |
| Windows | ARM64 | [zip](https://github.com/JE-DOG/figma-cli/releases/download/v0.0.1/figma-cli_0.0.1_windows_arm64.zip) |

Each archive contains the executable and this README. SHA-256 hashes are in
[checksums.txt](https://github.com/JE-DOG/figma-cli/releases/download/v0.0.1/checksums.txt).

For macOS or Linux, download and extract the archive, then place `figma-cli` in
a directory on your PATH. For example, on an Apple Silicon Mac:

```sh
curl -fLO https://github.com/JE-DOG/figma-cli/releases/download/v0.0.1/figma-cli_0.0.1_darwin_arm64.tar.gz
curl -fLO https://github.com/JE-DOG/figma-cli/releases/download/v0.0.1/checksums.txt
shasum -a 256 --check --ignore-missing checksums.txt
tar -xzf figma-cli_0.0.1_darwin_arm64.tar.gz
mkdir -p "$HOME/.local/bin"
install -m 755 figma-cli "$HOME/.local/bin/figma-cli"
export PATH="$HOME/.local/bin:$PATH"
figma-cli version
```

Use the archive for your platform from the table above. On Linux, you can use
`sha256sum --check --ignore-missing checksums.txt` to verify the download.
Persist the PATH setting in your shell configuration if needed.

On Windows, extract the ZIP and run `.\figma-cli.exe version` in PowerShell.
Add the directory containing `figma-cli.exe` to your user PATH to run it from
other directories.

Only network commands require a Figma token; see [Figma access](#figma-access).

## Local installation and updates from source

The Bash script [`sync-local.sh`](tools/scripts/setup/sync-local.sh) rebuilds the
CLI from the current checkout, updates `~/.local/bin/figma-cli`, and registers
the skill with a symlink at `~/.agents/skills/figma-cli`:

```sh
bash ~/projects/figma-cli/tools/scripts/setup/sync-local.sh
```

Run it again after updating the source code. It works from any working directory
and requires Go 1.26+; no Figma token is needed. The script does not pull changes
from git: update your checkout first using your preferred workflow. The binary
in `~/.local/bin` is replaced atomically only after a successful build and version
check. If the build fails, the existing installation is preserved. The script
does not update `bin/figma-cli` inside the checkout.

If `~/.local/bin` is not already in your PATH, add this to your shell configuration:

```sh
export PATH="$HOME/.local/bin:$PATH"
```

To use different directories or install only the binary:

```sh
bash tools/scripts/setup/sync-local.sh --bin-dir "$HOME/bin" --skills-dir "$HOME/.agents/skills"
bash tools/scripts/setup/sync-local.sh --no-skill
bash tools/scripts/setup/sync-local.sh --help
```

An existing skill symlink pointing to this checkout can be reused. An unrelated
directory, a symlink to another checkout, or a broken symlink causes an error
before the build or binary replacement; the script does not remove them. Choose
a different `--skills-dir` or use `--no-skill`. Shell settings, tokens, and other
CLIs are left unchanged; `sudo` is not required. Bash 3.2+ is supported.

To test installation scenarios in isolated temporary directories:

```sh
bash tools/scripts/setup/test-sync-local.sh
```

## Codex skill

The English-language [`figma-cli`](skills/figma-cli/SKILL.md) skill explains how
an agent should select nodes, inspect original components and mixed typography,
interpret layout, and export PNG/SVG files. Its instructions live in this
repository; `agents/openai.yaml` defines its display name in the skills list.

The script above registers the skill along with the CLI. To register only the
skill without building, create a symlink in the user skills directory,
`~/.agents/skills`:

```sh
mkdir -p "$HOME/.agents/skills"
ln -s "$HOME/projects/figma-cli/skills/figma-cli" "$HOME/.agents/skills/figma-cli"
```

If your checkout is elsewhere, adjust the path. The `ln -s` command does not
replace an existing registration; inspect it before reinstalling. The symlink
uses the current instructions from the repository without copying files. If you
move the checkout, update the symlink. When running commands from another
project, the skill locates `figma-cli` (including installations available through
PATH) and uses the absolute path to the binary.

Example Codex prompt:

```text
$figma-cli inspect the original components, fonts, and spacing at FIGMA_URL
```

The skill can also be selected automatically based on the task. If it does not
appear in the list, restart Codex. Pass the token to the CLI itself through
`FIGMA_TOKEN` in the process environment or `--token-stdin`; do not add it to the
skill or chat history. The skill exports assets when the task requests them.

User skill locations and symlink support are described in the
[official OpenAI documentation](https://learn.chatgpt.com/docs/build-skills).

## Why Go

| Criterion | Go | Rust |
| --- | --- | --- |
| HTTP, JSON, arguments, test servers | Standard library | Usually separate crates |
| Distribution | Single binary | Single binary |
| Resources | GC, sufficient for a network utility | Precise memory control without GC |
| Maintaining this CLI | Small codebase with no external dependencies | More setup for the same REST tasks |

Go was chosen; the implementation used the installed Go 1.26.1. Rust would make
sense if strict memory control or integration into an existing Rust project
became necessary.

## Build and verification

Go 1.26+ is required. There are no external Go dependencies.

```sh
cd ~/projects/figma-cli
go test -race ./...
go vet ./...
go build -o bin/figma-cli ./cmd/figma-cli
./bin/figma-cli help
```

The `httptest` tests use local HTTP/TLS servers and fictional data, so loopback
access is required. They do not contact Figma or require a token. This build
command places the binary in the git-ignored `bin/` directory. To install it in
your user PATH, use `tools/scripts/setup/sync-local.sh` as described above.

### Building a release

Maintainers can build all six release archives locally with Go 1.26+ and Python 3:

```sh
python3 tools/scripts/release/build.py 0.0.1
```

The output directory defaults to `dist/` and must be new or empty; use
`--output DIR` to choose another location. The script checks that the CLI version
matches the requested release and writes archives plus `checksums.txt`.

To publish a release, update `cliVersion` in `internal/cli/cli.go`, add notes at
`docs/releases/VERSION.md`, and push a matching `vVERSION` tag. The
[release workflow](.github/workflows/release.yml) runs tests and vet, builds all
archives, verifies checksums, smoke-tests the packaged Linux binary, and publishes
the GitHub Release with its assets.

## Figma access

Create a personal access token in Figma → Settings → Security → Personal access
tokens with the **`file_content:read`** scope and access to the required file.
Pass the token through the `FIGMA_TOKEN` environment variable from a secret
manager, or through `--token-stdin`. Configuration files and `.env` files are not
read automatically.

Example for zsh: input is hidden, and the value is not stored in command history.

```sh
read -rs 'FIGMA_TOKEN?Figma token: '
export FIGMA_TOKEN
./bin/figma-cli inspect 'https://www.figma.com/design/FILE_KEY/Screen?node-id=12-34'
unset FIGMA_TOKEN
```

Alternatively, pipe the token from your secret manager:

```sh
your-secret-manager-command | ./bin/figma-cli inspect --token-stdin --node 12:34 FILE_KEY
```

Here, `your-secret-manager-command` is a placeholder, not a CLI dependency.
stdin is read until EOF, up to 8 KiB; this flag explicitly overrides `FIGMA_TOKEN`.
For an existing OAuth access token, use `--auth oauth`; OAuth app creation,
sign-in, and token refresh are not implemented in the CLI.
By default, PATs are sent in `X-Figma-Token`, and OAuth tokens in
`Authorization: Bearer`. There is no command-line argument for the token itself.
Tokens are never saved, printed in diagnostics, or sent to the CDN during
downloads. Only GET requests are made to the API.

## Usage

Examples below use `./bin/figma-cli` from a source build. If you installed a
prebuilt binary on your PATH, use `figma-cli` instead.

All flags must appear **before** the final argument. Replace `FILE_KEY` and
`12:34` with your own values. Quote URLs because they may contain `?`, `&`, and `;`.

```sh
# Parse a link offline, without a token
./bin/figma-cli parse 'https://www.figma.com/design/FILE_KEY/Screen?node-id=12-34'

# Normalize URL IDs and select multiple nodes
./bin/figma-cli parse --node 12-34 --node '56:78,90:12' FILE_KEY

# Report on a selected subtree
./bin/figma-cli inspect --node 12:34 --depth 3 FILE_KEY

# Inspect the entire file; --max-nodes limits the report, not the API response
./bin/figma-cli inspect --max-nodes 500 FILE_KEY

# Include local size, relativeTransform, and vector paths from the API
./bin/figma-cli inspect --geometry --node 12:34 FILE_KEY

# Get the exact API response, including unknown fields and original numbers
./bin/figma-cli raw --node 12:34 FILE_KEY > response.json

# Fetch a specific version
./bin/figma-cli inspect --file-version VERSION_ID --node 12:34 FILE_KEY

# Download a PNG at 2x; creates exports/12-34.png
./bin/figma-cli export --node 12:34 --scale 2 FILE_KEY

# SVG export converts text to outlines by default
./bin/figma-cli export --node 12:34 --format svg --output assets FILE_KEY

# Preserve SVG <text>; appearance depends on fonts and the renderer
./bin/figma-cli export --node 12:34 --format svg --svg-text FILE_KEY

# Use full bounds instead of cropping the text node
./bin/figma-cli export --node 12:34 --absolute-bounds FILE_KEY
```

The CLI accepts file/branch keys and HTTPS URLs on `figma.com` / `www.figma.com`
with `file`, `design`, `proto`, `board`, or `slides` paths. This extracts the key;
it does not imply support for every editor feature through REST. For a URL such
as `/design/MAIN_KEY/branch/BRANCH_KEY/Name`, `BRANCH_KEY` is used for API requests.
The `--node` flags override `node-id` from the URL; duplicates are removed while
preserving order. Standard ID pairs and compound instance IDs such as
`I12:34;56:78` are supported; `12-34` is normalized to `12:34`. REST does not
publish a complete ID grammar, so future formats may require parser updates.

The `parse`, `inspect`, and `export` commands output formatted JSON. `raw` returns
the API JSON without transforming fields or numbers. Diagnostics go to stderr;
errors return exit code 1. For a missing/null node, `inspect` fails; `raw` preserves
the original response, including null.

## Report contents

`nodes` is a flat list in traversal order, with `id`, `name`, `type`, and
`parentId`. The root node's parent is unknown in a node API response. Overlapping
subtrees do not duplicate the same ID.

- `component` on an INSTANCE links `componentId` to the `components` table:
  original name, key, remote status, and component set name, when metadata is
  available. A renamed instance keeps its own `name`; the source name is in
  `component.name`. When metadata is missing, `resolved=false`, with no fallback
  to the instance name and no additional library requests. A detached instance
  no longer contains a reliable reference to its original component.
- `typography` for TEXT/TEXT_PATH preserves the complete base `style`, including
  font family/PostScript name, size, style/weight, spacing, and line height.
  `runs` merge the base style with partial overrides, each with text, an override
  ID, and half-open `start`/`end` offsets. A zero ID or missing entries at the end
  of the array indicate the base style. If an override is missing from the table,
  a warning is added and the base style is retained for that run.
- `layout` contains API-provided padding, gaps, auto-layout and grid settings,
  sizing/alignment, constraints, min/max values, bounding boxes, radii, and optional
  vector geometry. Missing fields are not replaced with zeros; `0` and `null`
  are preserved. `boundVariables` contains references, not resolved values.
- `units`, `limitations`, `warnings`, and `truncated` make limitations explicit.

Example query using the optional `jq` tool:

```sh
./bin/figma-cli inspect --node 12:34 FILE_KEY |
  jq '.nodes[] | select(.component) | {name, source: .component.name}'
```

## Units and limitations

- Geometry, padding/gaps, font size, and letter spacing use **Figma pixels**.
  Converting to Android dp/sp requires an agreed design scale and platform
  settings; conversion is not performed automatically.
- `absoluteBoundingBox` contains canvas bounds after transformations;
  `absoluteRenderBounds` accounts for visual effects and may be null.
  `size` and `relativeTransform` are requested with `--geometry`; for rotated
  nodes, the bounding box size differs from the local size.
- Padding/gaps are layout settings, not computed distances between arbitrary
  layers. SPACE_BETWEEN, wrap, grid, and nested transforms require a layout
  engine; the CLI does not infer arbitrary margins or distances.
- REST `lineHeightUnit` values are `PIXELS`, `FONT_SIZE_%`, and `INTRINSIC_%`.
  The corresponding original numeric fields are preserved, including the
  deprecated `lineHeightPercent`; `AUTO` is not synthesized. `FONT_SIZE_%` and
  `INTRINSIC_%` are distinct.
- Mixed text uses **UTF-16** indexing, as in the Figma Plugin API. REST describes
  “characters” but does not explicitly guarantee UTF-16; this assumption requires
  verification against a live REST response containing emoji. Fixtures test this
  model, short override arrays, and surrogate pairs. A boundary that splits a
  surrogate pair produces a warning: the entire character receives the style of
  its first code unit.
- SVG text depends on installed fonts. PNG scale ranges from 0.01 to 4; SVG scale
  stays at 1. Figma may downscale exports larger than 32 megapixels. A null render
  URL indicates a node error; all such results are checked before downloading.

## Limits, errors, and storage

By default, `--timeout 30s` limits the entire network command: requests, waits,
and sequential downloads. Ctrl+C cancels the request or wait.
`--retries 2` sets retries for 429 responses only (allowed range: 0–5).
`Retry-After` values in seconds and HTTP-date format are honored; a missing or
invalid header results in waits of 1/2/4… seconds. If a wait would exceed the
timeout, the command returns an error indicating the required wait. Other HTTP
errors are not retried automatically.

Limits: 100 selected IDs, JSON up to 64 MiB, and individual assets up to 128 MiB.
In `inspect`, text per node is limited to 256 KiB in UTF-8, and the entire report
is limited to 10000 style runs. Exceeding these limits produces an explicit error
before the report grows further in memory: select a smaller subtree or use `raw`
for the full response. `--max-nodes` defaults to 2000 (range: 1–100000) and limits
only report traversal, with an explicit `truncated=true`. `--depth 0` means no API
depth limit; values from 1 to 1000 are passed to the API. The file and nodes
endpoints count depth differently; see the documentation. For large files, select
specific nodes and use `--depth`. `raw` does not apply `--max-nodes`, but retains
the HTTP response size limit.

HTTP error bodies and signed CDN URLs are not printed. For 401/403 errors, check
token expiration, scope, and access; for 404 errors, check the file/branch. Figma
limits depend on the plan, resource, and seat: increasing retries does not
increase the quota.

Exports are downloaded to a temporary file in the destination directory and
published atomically without replacing existing files. The file system must
support hard links. A failed download leaves no partial file; previously
completed exports remain. The manifest contains only IDs and local paths.
API responses and images may contain private designs: choose storage locations
carefully. `exports/`, `bin/`, and `.env*` are ignored by git.

## Official sources

Checked during implementation on October 25, 2026:

- [File/nodes/images endpoints, depth, null nodes, and image scale](https://developers.figma.com/docs/rest-api/file-endpoints/)
- [Node types: INSTANCE, TEXT, and characterStyleOverrides](https://developers.figma.com/docs/rest-api/file-node-types/)
- [TypeStyle, units, and component metadata](https://developers.figma.com/docs/rest-api/file-property-types/)
- [PATs and X-Figma-Token](https://developers.figma.com/docs/rest-api/personal-access-tokens/)
- [Authentication and scopes](https://developers.figma.com/docs/rest-api/authentication/)
- [Rate limits and Retry-After](https://developers.figma.com/docs/rest-api/rate-limits/)
- [Plugin API: UTF-16 and surrogate pairs](https://developers.figma.com/docs/plugins/api/properties/TextNode-deletecharacters/)
- [Official MCP guide: file/branch URL parsing](https://github.com/figma/mcp-server-guide/blob/main/skills-figquery/figma-code-connect/SKILL.md)

Checks without a token verify the local implementation. A live smoke test
requires your token through env/stdin and the URL of an accessible file; no live
verification was performed without them.
