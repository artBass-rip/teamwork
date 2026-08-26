#!/bin/sh
set -eu

root_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
go_command=${GO:-go}

for platform in darwin-arm64 darwin-amd64 linux-arm64 linux-amd64; do
  target_os=${platform%-*}
  target_arch=${platform#*-}
  output="$root_dir/dist/$platform"
  mkdir -p "$output/modules/teamwork.echo/bin/$platform" "$output/modules/teamwork.activity/bin/$platform"
  mkdir -p "$output/modules/teamwork.project-view/bin/$platform"
  mkdir -p "$output/modules/teamwork.jira/bin/$platform"
  (cd "$root_dir/core" && CGO_ENABLED=0 GOOS="$target_os" GOARCH="$target_arch" "$go_command" build -trimpath -ldflags='-s -w' -o "$output/teamwork" ./cmd/teamwork)
  (cd "$root_dir/modules/echo-go" && CGO_ENABLED=0 GOOS="$target_os" GOARCH="$target_arch" "$go_command" build -trimpath -ldflags='-s -w' -o "$output/modules/teamwork.echo/bin/$platform/teamwork-echo" .)
  (cd "$root_dir/modules/activity-go" && CGO_ENABLED=0 GOOS="$target_os" GOARCH="$target_arch" "$go_command" build -trimpath -ldflags='-s -w' -o "$output/modules/teamwork.activity/bin/$platform/teamwork-activity" .)
  (cd "$root_dir/modules/project-view-go" && CGO_ENABLED=0 GOOS="$target_os" GOARCH="$target_arch" "$go_command" build -trimpath -ldflags='-s -w' -o "$output/modules/teamwork.project-view/bin/$platform/teamwork-project-view" .)
  (cd "$root_dir/modules/jira-go" && CGO_ENABLED=0 GOOS="$target_os" GOARCH="$target_arch" "$go_command" build -trimpath -ldflags='-s -w' -o "$output/modules/teamwork.jira/bin/$platform/teamwork-jira" .)
  cp "$root_dir/modules/echo-go/module.json" "$output/modules/teamwork.echo/module.json"
  cp "$root_dir/modules/activity-go/module.json" "$output/modules/teamwork.activity/module.json"
  cp "$root_dir/modules/project-view-go/module.json" "$output/modules/teamwork.project-view/module.json"
  cp "$root_dir/modules/jira-go/module.json" "$output/modules/teamwork.jira/module.json"
done

echo "Release binaries created in dist"
