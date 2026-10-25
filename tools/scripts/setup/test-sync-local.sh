#!/usr/bin/env bash
# Local-only integration tests: a fake Go command prevents builds and network use.
# Compatible with the Bash 3.2 shipped by macOS.
set -eu
set -o pipefail

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd -P)
SCRIPT_UNDER_TEST=${1:-"$SCRIPT_DIR/sync-local.sh"}
if [ "$#" -gt 1 ] || [ ! -f "$SCRIPT_UNDER_TEST" ]; then
  printf 'Usage: %s [path/to/sync-local.sh]\n' "$0" >&2
  exit 2
fi
SCRIPT_UNDER_TEST=$(CDPATH= cd -- "$(dirname -- "$SCRIPT_UNDER_TEST")" && printf '%s/%s\n' "$(pwd -P)" "$(basename -- "$SCRIPT_UNDER_TEST")")
ORIGINAL_PATH=$PATH
WORK=$(mktemp -d "${TMPDIR:-/tmp}/figma-cli-sync-test.XXXXXX")
WORK=$(CDPATH= cd -- "$WORK" && pwd -P)
trap 'rm -rf -- "$WORK"' EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
PASS_COUNT=0

fail() {
  printf 'FAIL: %s\n' "$*" >&2
  if [ -f "${CASE_ROOT:-}/output" ]; then
    cat -- "$CASE_ROOT/output" >&2
  fi
  exit 1
}

assert_file() {
  [ -f "$1" ] || fail "expected file: $1"
}

assert_absent() {
  [ ! -e "$1" ] && [ ! -L "$1" ] || fail "unexpected path: $1"
}

assert_equal() {
  [ "$1" = "$2" ] || fail "$3: expected <$2>, got <$1>"
}

pass() {
  PASS_COUNT=$((PASS_COUNT + 1))
  printf 'ok %s - %s\n' "$PASS_COUNT" "$1"
}

fixture() {
  CASE_ROOT=$(mktemp -d "$WORK/fixture.XXXXXX")
  REPO="$CASE_ROOT/repo with spaces"
  TEST_HOME="$CASE_ROOT/home with spaces"
  FAKE_BIN="$CASE_ROOT/fake tools"
  UNRELATED="$CASE_ROOT/unrelated working directory"
  TEST_SCRIPT="$REPO/tools/scripts/setup/sync-local.sh"
  GO_LOG="$CASE_ROOT/go.log"
  mkdir -p -- "$REPO/tools/scripts/setup" "$REPO/cmd/figma-cli" \
    "$REPO/skills/figma-cli" "$TEST_HOME" "$FAKE_BIN" "$UNRELATED"
  cp -- "$SCRIPT_UNDER_TEST" "$TEST_SCRIPT"
  printf 'module example.com/fixture\n\ngo 1.26\n' > "$REPO/go.mod"
  printf '# fixture skill\n' > "$REPO/skills/figma-cli/SKILL.md"
  cat > "$FAKE_BIN/go" <<'FAKE_GO'
#!/usr/bin/env bash
set -eu
[ "${GOTOOLCHAIN:-}" = local ] || { printf 'toolchain is not local\n' >&2; exit 91; }
[ "$#" -eq 6 ] || { printf 'unexpected Go arguments\n' >&2; exit 92; }
[ "$1" = -C ] && [ "$2" = "$FAKE_REPO" ] && [ "$3" = build ] && \
  [ "$4" = -o ] && [ "$6" = ./cmd/figma-cli ] || exit 93
printf 'build\n' >> "$FAKE_GO_LOG"
case "${FAKE_GO_MODE:-ok}" in
  fail)
    printf 'partial binary\n' > "$5"
    printf 'fixture build failure\n' >&2
    exit 23
    ;;
  smoke-fail)
    printf '#!/usr/bin/env bash\nexit 24\n' > "$5"
    ;;
  bad-version)
    printf '#!/usr/bin/env bash\nprintf "unrelated-tool fixture\\n"\n' > "$5"
    ;;
  ok)
    cat > "$5" <<BINARY
#!/usr/bin/env bash
[ "\$#" -eq 1 ] && [ "\$1" = version ] || exit 94
printf 'figma-cli fixture-${FAKE_BUILD_STAMP:-one}\\n'
BINARY
    ;;
  *) exit 95 ;;
esac
# The setup script owns chmod and must make the freshly built file executable.
chmod 600 "$5"
FAKE_GO
  chmod 755 "$FAKE_BIN/go"
  FAKE_GO_MODE=ok
  FAKE_BUILD_STAMP=one
}

run_sync() {
  (
    cd -- "$UNRELATED"
    HOME="$TEST_HOME" PATH="$FAKE_BIN:$ORIGINAL_PATH" \
      FAKE_REPO="$REPO" FAKE_GO_LOG="$GO_LOG" \
      FAKE_GO_MODE="$FAKE_GO_MODE" FAKE_BUILD_STAMP="$FAKE_BUILD_STAMP" \
      bash "$TEST_SCRIPT" "$@"
  ) > "$CASE_ROOT/output" 2>&1
}

expect_failure() {
  if run_sync "$@"; then
    fail 'setup unexpectedly succeeded'
  fi
}

assert_no_temp_files() {
  # Each test's binary directory must contain only its installed binary.
  local directory=$1
  local entry
  for entry in "$directory"/* "$directory"/.[!.]* "$directory"/..?*; do
    [ -e "$entry" ] || [ -L "$entry" ] || continue
    [ "$(basename -- "$entry")" = figma-cli ] || fail "temporary file left behind: $entry"
  done
}

assert_default_install() {
  local binary="$TEST_HOME/.local/bin/figma-cli"
  local skill="$TEST_HOME/.agents/skills/figma-cli"
  assert_file "$binary"
  [ -x "$binary" ] || fail 'installed binary is not executable'
  assert_equal "$("$binary" version)" "figma-cli fixture-$FAKE_BUILD_STAMP" 'installed version'
  [ -L "$skill" ] || fail 'skill is not a symbolic link'
  assert_equal "$(CDPATH= cd -- "$skill" && pwd -P)" "$REPO/skills/figma-cli" 'skill target'
  assert_absent "$REPO/bin"
  assert_no_temp_files "$TEST_HOME/.local/bin"
}

fixture
run_sync || fail 'first default install failed'
assert_default_install
assert_equal "$(cat -- "$GO_LOG")" build 'first build invocation'
pass 'default install from unrelated CWD and paths containing spaces'

FAKE_BUILD_STAMP=two
run_sync || fail 'reinstall failed'
assert_default_install
assert_equal "$(wc -l < "$GO_LOG" | tr -d ' ')" 2 'reinstall must build again'
pass 'rerun refreshes the binary and accepts its existing skill link'

fixture
CUSTOM_BIN="$CASE_ROOT/custom binary directory"
CUSTOM_SKILLS="$CASE_ROOT/custom skill directory"
run_sync --bin-dir "$CUSTOM_BIN" --skills-dir "$CUSTOM_SKILLS" || fail 'custom install failed'
assert_file "$CUSTOM_BIN/figma-cli"
[ -L "$CUSTOM_SKILLS/figma-cli" ] || fail 'custom skill link missing'
assert_equal "$(CDPATH= cd -- "$CUSTOM_SKILLS/figma-cli" && pwd -P)" "$REPO/skills/figma-cli" 'custom skill target'
assert_absent "$TEST_HOME/.local"
assert_absent "$TEST_HOME/.agents"
assert_absent "$REPO/bin"
assert_no_temp_files "$CUSTOM_BIN"
pass 'custom binary and skill directories'

fixture
rm -rf -- "$REPO/skills"
CUSTOM_BIN="$CASE_ROOT/no skill bin"
CUSTOM_SKILLS="$CASE_ROOT/unused skills"
run_sync --no-skill --bin-dir "$CUSTOM_BIN" --skills-dir "$CUSTOM_SKILLS" || fail '--no-skill failed without skill source'
assert_file "$CUSTOM_BIN/figma-cli"
assert_absent "$CUSTOM_SKILLS"
assert_absent "$TEST_HOME/.agents"
assert_no_temp_files "$CUSTOM_BIN"
pass '--no-skill neither requires nor creates a skill'

fixture
mkdir -p -- "$TEST_HOME/.agents/skills"
RELATIVE_TARGET='../../../repo with spaces/skills/figma-cli'
ln -s -- "$RELATIVE_TARGET" "$TEST_HOME/.agents/skills/figma-cli"
run_sync || fail 'existing relative skill link was rejected'
assert_default_install
assert_equal "$(readlink "$TEST_HOME/.agents/skills/figma-cli")" "$RELATIVE_TARGET" 'existing relative link must be preserved'
pass 'existing relative symlink to the correct skill is accepted'

for FAILURE_MODE in fail smoke-fail bad-version; do
  fixture
  run_sync || fail 'fixture initial install failed'
  cp -- "$TEST_HOME/.local/bin/figma-cli" "$CASE_ROOT/binary-before"
  SKILL_BEFORE=$(readlink "$TEST_HOME/.agents/skills/figma-cli")
  FAKE_GO_MODE=$FAILURE_MODE
  expect_failure
  cmp -s -- "$CASE_ROOT/binary-before" "$TEST_HOME/.local/bin/figma-cli" || fail "$FAILURE_MODE changed installed binary"
  assert_equal "$(readlink "$TEST_HOME/.agents/skills/figma-cli")" "$SKILL_BEFORE" "$FAILURE_MODE changed skill link"
  assert_no_temp_files "$TEST_HOME/.local/bin"
  pass "$FAILURE_MODE preserves previous installation and removes temporary files"
done

for CONFLICT in directory different-symlink broken-symlink; do
  fixture
  mkdir -p -- "$TEST_HOME/.local/bin" "$TEST_HOME/.agents/skills"
  printf 'existing binary sentinel\n' > "$TEST_HOME/.local/bin/figma-cli"
  cp -- "$TEST_HOME/.local/bin/figma-cli" "$CASE_ROOT/binary-before"
  SKILL="$TEST_HOME/.agents/skills/figma-cli"
  case "$CONFLICT" in
    directory)
      mkdir -p -- "$SKILL"
      printf 'foreign skill sentinel\n' > "$SKILL/SKILL.md"
      ;;
    different-symlink)
      mkdir -p -- "$CASE_ROOT/foreign skill"
      ln -s -- "$CASE_ROOT/foreign skill" "$SKILL"
      ;;
    broken-symlink)
      ln -s -- "$CASE_ROOT/missing skill" "$SKILL"
      ;;
  esac
  expect_failure
  assert_absent "$GO_LOG"
  cmp -s -- "$CASE_ROOT/binary-before" "$TEST_HOME/.local/bin/figma-cli" || fail "$CONFLICT changed installed binary"
  case "$CONFLICT" in
    directory)
      [ -d "$SKILL" ] && [ ! -L "$SKILL" ] || fail 'foreign skill directory replaced'
      assert_equal "$(cat -- "$SKILL/SKILL.md")" 'foreign skill sentinel' 'foreign skill directory content'
      ;;
    different-symlink)
      assert_equal "$(readlink "$SKILL")" "$CASE_ROOT/foreign skill" 'foreign skill symlink'
      ;;
    broken-symlink)
      assert_equal "$(readlink "$SKILL")" "$CASE_ROOT/missing skill" 'broken skill symlink'
      ;;
  esac
  assert_no_temp_files "$TEST_HOME/.local/bin"
  pass "$CONFLICT is rejected before building or replacing the binary"
done

fixture
run_sync --help || fail '--help failed'
assert_absent "$GO_LOG"
assert_absent "$TEST_HOME/.local"
assert_absent "$TEST_HOME/.agents"
for ARGUMENT in --unknown --bin-dir --skills-dir unexpected-positional; do
  expect_failure "$ARGUMENT"
  assert_absent "$GO_LOG"
  assert_absent "$TEST_HOME/.local"
  assert_absent "$TEST_HOME/.agents"
done
expect_failure --bin-dir ''
expect_failure --skills-dir ''
assert_absent "$GO_LOG"
assert_absent "$TEST_HOME/.local"
assert_absent "$TEST_HOME/.agents"
pass 'help and invalid flags do not build or install anything'

fixture
rm -- "$REPO/go.mod"
expect_failure
assert_absent "$GO_LOG"
assert_absent "$TEST_HOME/.local"
assert_absent "$TEST_HOME/.agents"
pass 'missing Go module is rejected before installation'

fixture
rm -- "$REPO/skills/figma-cli/SKILL.md"
expect_failure
assert_absent "$GO_LOG"
assert_absent "$TEST_HOME/.local"
assert_absent "$TEST_HOME/.agents"
pass 'missing skill source is rejected before installation'

printf 'All %s local setup integration tests passed.\n' "$PASS_COUNT"
