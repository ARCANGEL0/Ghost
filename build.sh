#!/usr/bin/env bash
set -euo pipefail

rootDir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
go build -o "$rootDir/ghost" "$rootDir/main.go"
