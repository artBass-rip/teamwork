#!/bin/sh
set -eu

root_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
binary="$root_dir/core/bin/teamwork"

if [ ! -x "$binary" ]; then
  echo "TeamWork binary is not built. Run ./scripts/build.sh first." >&2
  exit 1
fi

TEAMWORK_MODULES_DIR="${TEAMWORK_MODULES_DIR:-$root_dir/modules}" exec "$binary" serve "$@"
