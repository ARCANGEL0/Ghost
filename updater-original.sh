#!/bin/sh
set -eu

# Update Ghost from the original swadhinbiswas repository.

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
GHOST_REPO="swadhinbiswas/Ghost" \
  exec "$SCRIPT_DIR/install.sh" "$@"
