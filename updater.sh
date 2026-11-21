#!/bin/sh
set -eu

# Update Ghost from the ARCANGEL0 fork.

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
GHOST_REPO="ARCANGEL0/ghost" \
  exec "$SCRIPT_DIR/install.sh" "$@"
