#!/usr/bin/env bash
# Build and install this checkout for the current user. Compatible with Bash 3.2+.
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage: sync-local.sh [--bin-dir DIR] [--skills-dir DIR] [--no-skill]

Build figma-cli from this checkout and atomically update the local executable.
Also register its skill with a symlink unless --no-skill is specified.

  --bin-dir DIR     Executable directory (default: $HOME/.local/bin)
  --skills-dir DIR  User skill registry (default: $HOME/.agents/skills)
  --no-skill        Install only the executable
  -h, --help        Show this help without making changes

Requires Go 1.26+. No token, Figma connection or sudo is needed.
An existing skill registration must already point to this checkout.
USAGE
}

fail() {
  printf 'sync-local: %s\n' "$*" >&2
  exit 1
}

bin_dir=''
skills_dir=''
register_skill=1
while [ "$#" -gt 0 ]; do
  case "$1" in
    --bin-dir|--skills-dir)
      [ "$#" -ge 2 ] && [ -n "$2" ] || fail "$1 requires a directory"
      case "$2" in --*) fail "$1 requires a directory" ;; esac
      if [ "$1" = '--bin-dir' ]; then bin_dir="$2"; else skills_dir="$2"; fi
      shift 2
      ;;
    --no-skill) register_skill=0; shift ;;
    -h|--help) usage; exit 0 ;;
    *) fail 'unknown argument; use --help' ;;
  esac
done

if [ -z "$bin_dir" ]; then
  [ -n "${HOME:-}" ] || fail 'HOME is unset; provide --bin-dir'
  bin_dir="$HOME/.local/bin"
fi
if [ "$register_skill" -eq 1 ] && [ -z "$skills_dir" ]; then
  [ -n "${HOME:-}" ] || fail 'HOME is unset; provide --skills-dir'
  skills_dir="$HOME/.agents/skills"
fi

script_dir="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
repo_root="$(CDPATH= cd -- "$script_dir/../../.." && pwd -P)"
[ -f "$repo_root/go.mod" ] && [ -d "$repo_root/cmd/figma-cli" ] || fail 'run the script from a figma-cli checkout'
command -v go >/dev/null 2>&1 || fail 'Go 1.26+ is required on PATH'
skill_source="$repo_root/skills/figma-cli"
stage_dir=''

cleanup() {
  if [ -n "$stage_dir" ]; then rm -rf -- "$stage_dir"; fi
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

current_skill() {
  [ -L "$skills_dir/figma-cli" ] &&
    [ "$(CDPATH= cd -- "$skills_dir/figma-cli" 2>/dev/null && pwd -P)" = "$skill_source" ]
}

# Reject conflicting registrations before building or replacing an executable.
if [ "$register_skill" -eq 1 ]; then
  [ -f "$skill_source/SKILL.md" ] || fail 'checkout is missing skills/figma-cli/SKILL.md'
  if [ -e "$skills_dir/figma-cli" ] || [ -L "$skills_dir/figma-cli" ]; then
    current_skill || fail 'skill registration belongs to another location; use --no-skill or another --skills-dir'
  fi
fi
[ ! -d "$bin_dir/figma-cli" ] || fail 'executable destination is a directory'
mkdir -p -- "$bin_dir"
bin_dir="$(CDPATH= cd -- "$bin_dir" && pwd -P)"
if [ "$register_skill" -eq 1 ]; then
  mkdir -p -- "$skills_dir"
  skills_dir="$(CDPATH= cd -- "$skills_dir" && pwd -P)"
fi

# Stage on the destination filesystem so publishing the executable is atomic.
stage_dir="$(mktemp -d "$bin_dir/.figma-cli-build.XXXXXX")"
staged_bin="$stage_dir/figma-cli"
GOTOOLCHAIN=local go -C "$repo_root" build -o "$staged_bin" ./cmd/figma-cli
chmod 755 "$staged_bin"
version="$("$staged_bin" version)" || fail 'built executable failed its version check'
case "$version" in 'figma-cli '*) ;; *) fail 'built executable is not figma-cli' ;; esac

if [ "$register_skill" -eq 1 ] && ! current_skill; then
  # Target the parent directory: an existing final entry causes EEXIST rather
  # than following a racing directory/symlink and writing inside another skill.
  if ! ln -s "$skill_source" "$skills_dir/" 2>/dev/null; then
    current_skill || fail 'could not register skill; existing registration was preserved'
  fi
fi
mv -f "$staged_bin" "$bin_dir/"
printf 'Installed %s (%s)\n' "$bin_dir/figma-cli" "$version"
if [ "$register_skill" -eq 1 ]; then
  printf 'Skill: %s -> %s\n' "$skills_dir/figma-cli" "$skill_source"
fi
case ":${PATH:-}:" in
  *":${bin_dir}:"*) ;;
  *) printf 'Add this directory to PATH to invoke figma-cli: %s\n' "$bin_dir" ;;
esac
