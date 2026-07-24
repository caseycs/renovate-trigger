#!/usr/bin/env bash
#
# Guard against the Go version drifting between go.mod and the Dockerfile.
#
# go.mod pins a full version ("go 1.25.5") — that's the language/toolchain
# floor. The Dockerfile builder pins the image to major.minor
# ("golang:1.25-alpine") so it tracks the latest patch automatically. So we
# can't compare the strings verbatim; we compare the major.minor, which is the
# part that actually has to agree for the build to use the right Go release.
set -euo pipefail

cd "$(dirname "$0")/.."

# "go 1.25.5" -> "1.25"
gomod_ver=$(grep -E '^go [0-9]' go.mod | awk '{print $2}' | cut -d. -f1,2)

# "FROM ... golang:1.25-alpine ..." -> "1.25"
dockerfile_ver=$(grep -oE 'golang:[0-9]+\.[0-9]+' Dockerfile | head -1 | cut -d: -f2)

if [[ -z "$gomod_ver" ]]; then
  echo "could not parse Go version from go.mod" >&2
  exit 1
fi
if [[ -z "$dockerfile_ver" ]]; then
  echo "could not parse golang image version from Dockerfile" >&2
  exit 1
fi

if [[ "$gomod_ver" != "$dockerfile_ver" ]]; then
  echo "Go version mismatch:" >&2
  echo "  go.mod      -> $gomod_ver" >&2
  echo "  Dockerfile  -> $dockerfile_ver" >&2
  exit 1
fi

echo "Go version matches: $gomod_ver (go.mod and Dockerfile)"
