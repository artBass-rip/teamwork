#!/bin/sh
set -eu

root_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
go_command=${GO:-go}
target_os=${GOOS:-$("$go_command" env GOOS)}
target_arch=${GOARCH:-$("$go_command" env GOARCH)}
platform="$target_os-$target_arch"

mkdir -p "$root_dir/core/bin"
(cd "$root_dir/core" && CGO_ENABLED=0 GOOS="$target_os" GOARCH="$target_arch" "$go_command" build -trimpath -ldflags='-s -w' -o bin/teamwork ./cmd/teamwork)

for module_dir in "$root_dir"/modules/*; do
  [ -f "$module_dir/go.mod" ] || continue
  binary_path=$(sed -n "s/.*\"$platform\": *\"\([^\"]*\)\".*/\1/p" "$module_dir/module.json" | head -n 1)
  [ -n "$binary_path" ] || { echo "Missing $platform executable path in $module_dir/module.json" >&2; exit 1; }
  mkdir -p "$module_dir/$(dirname "$binary_path")"
  (cd "$module_dir" && CGO_ENABLED=0 GOOS="$target_os" GOARCH="$target_arch" "$go_command" build -trimpath -ldflags='-s -w' -o "$binary_path" .)
done

echo "Built TeamWork core and modules for $platform"
