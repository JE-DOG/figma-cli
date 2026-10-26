#!/usr/bin/env python3
"""Build release archives using Go and the Python standard library."""

import argparse
import hashlib
import os
from pathlib import Path
import re
import subprocess
import tarfile
import tempfile
import zipfile


TARGETS = (
    ("darwin", "amd64"),
    ("darwin", "arm64"),
    ("linux", "amd64"),
    ("linux", "arm64"),
    ("windows", "amd64"),
    ("windows", "arm64"),
)
REPO_ROOT = Path(__file__).resolve().parents[3]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("version", help="release version, e.g. 0.0.1")
    parser.add_argument("--output", type=Path, default=REPO_ROOT / "dist")
    args = parser.parse_args()
    if not re.fullmatch(r"[0-9]+\.[0-9]+\.[0-9]+", args.version):
        parser.error("version must have the form MAJOR.MINOR.PATCH")
    output = args.output.resolve()
    if output.exists() and (not output.is_dir() or any(output.iterdir())):
        parser.error("output must be a new or empty directory")

    # Verify the version users will see, rather than just naming the archives.
    env = dict(os.environ, CGO_ENABLED="0")
    for key in ("GOOS", "GOARCH", "GOARM", "GOAMD64", "GOARM64", "GOFLAGS"):
        env.pop(key, None)
    actual = subprocess.check_output(
        ["go", "run", "./cmd/figma-cli", "version"],
        cwd=REPO_ROOT,
        env=env,
        text=True,
    ).strip()
    if actual != f"figma-cli {args.version}":
        parser.error(f"CLI reports {actual!r}; expected figma-cli {args.version}")

    output.mkdir(parents=True, exist_ok=True)
    checksums = []
    with tempfile.TemporaryDirectory(prefix="figma-cli-release-") as temporary:
        for goos, goarch in TARGETS:
            name = "figma-cli.exe" if goos == "windows" else "figma-cli"
            binary = Path(temporary) / name
            print(f"Building {goos}/{goarch}", flush=True)
            subprocess.run(
                ["go", "build", "-trimpath", "-ldflags=-s -w", "-o", str(binary),
                 "./cmd/figma-cli"],
                cwd=REPO_ROOT,
                env=dict(env, GOOS=goos, GOARCH=goarch),
                check=True,
            )
            files = ((binary, name), (REPO_ROOT / "README.md", "README.md"))
            stem = f"figma-cli_{args.version}_{goos}_{goarch}"
            if goos == "windows":
                archive = output / f"{stem}.zip"
                with zipfile.ZipFile(archive, "w", zipfile.ZIP_DEFLATED) as package:
                    for source, destination in files:
                        package.write(source, destination)
            else:
                binary.chmod(0o755)
                archive = output / f"{stem}.tar.gz"
                with tarfile.open(archive, "w:gz") as package:
                    for source, destination in files:
                        package.add(source, arcname=destination)
            digest = hashlib.sha256(archive.read_bytes()).hexdigest()
            checksums.append(f"{digest}  {archive.name}\n")
    (output / "checksums.txt").write_text("".join(checksums), encoding="utf-8")
    print(f"Release archives and checksums: {output}")


if __name__ == "__main__":
    main()
