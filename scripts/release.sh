#!/bin/sh
set -eu

root_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
go_command=${GO:-go}

for platform in darwin-arm64 darwin-amd64 linux-arm64 linux-amd64; do
  target_os=${platform%-*}
  target_arch=${platform#*-}
  output="$root_dir/dist/$platform"
  mkdir -p "$output/modules"
  (cd "$root_dir/core" && CGO_ENABLED=0 GOOS="$target_os" GOARCH="$target_arch" "$go_command" build -trimpath -ldflags='-s -w' -o "$output/teamwork" ./cmd/teamwork)
  for module_dir in "$root_dir"/modules/*; do
    [ -f "$module_dir/module.json" ] || continue
    module_id=$(sed -n 's/.*"id": *"\([^"]*\)".*/\1/p' "$module_dir/module.json" | head -n 1)
    binary_path=$(sed -n "s/.*\"$platform\": *\"\([^\"]*\)\".*/\1/p" "$module_dir/module.json" | head -n 1)
    [ -n "$module_id" ] || { echo "Missing module id in $module_dir/module.json" >&2; exit 1; }
    [ -n "$binary_path" ] || continue
    module_output="$output/modules/$module_id"
    mkdir -p "$module_output/$(dirname "$binary_path")"
    if [ -f "$module_dir/go.mod" ]; then
      (cd "$module_dir" && CGO_ENABLED=0 GOOS="$target_os" GOARCH="$target_arch" "$go_command" build -trimpath -ldflags='-s -w' -o "$module_output/$binary_path" .)
    elif [ -f "$module_dir/$binary_path" ]; then
      cp "$module_dir/$binary_path" "$module_output/$binary_path"
    else
      echo "Missing $platform executable for $module_id: $module_dir/$binary_path" >&2
      exit 1
    fi
    cp "$module_dir/module.json" "$module_output/module.json"
  done
done

echo "Release binaries created in dist"
