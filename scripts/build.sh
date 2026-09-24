#!/bin/sh
set -eu

root_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
go_command=${GO:-go}
target_os=${GOOS:-$("$go_command" env GOOS)}
target_arch=${GOARCH:-$("$go_command" env GOARCH)}
platform="$target_os-$target_arch"

mkdir -p "$root_dir/core/bin"
mkdir -p "$root_dir/modules/echo-go/bin/$platform"
mkdir -p "$root_dir/modules/activity-go/bin/$platform"
mkdir -p "$root_dir/modules/project-view-go/bin/$platform"
mkdir -p "$root_dir/modules/jira-go/bin/$platform"
mkdir -p "$root_dir/modules/slack-go/bin/$platform"
mkdir -p "$root_dir/modules/onenote-go/bin/$platform"
mkdir -p "$root_dir/modules/slack-onenote-go/bin/$platform"

(cd "$root_dir/core" && CGO_ENABLED=0 GOOS="$target_os" GOARCH="$target_arch" "$go_command" build -trimpath -ldflags='-s -w' -o bin/teamwork ./cmd/teamwork)
(cd "$root_dir/modules/echo-go" && CGO_ENABLED=0 GOOS="$target_os" GOARCH="$target_arch" "$go_command" build -trimpath -ldflags='-s -w' -o "bin/$platform/teamwork-echo" .)
(cd "$root_dir/modules/activity-go" && CGO_ENABLED=0 GOOS="$target_os" GOARCH="$target_arch" "$go_command" build -trimpath -ldflags='-s -w' -o "bin/$platform/teamwork-activity" .)
(cd "$root_dir/modules/project-view-go" && CGO_ENABLED=0 GOOS="$target_os" GOARCH="$target_arch" "$go_command" build -trimpath -ldflags='-s -w' -o "bin/$platform/teamwork-project-view" .)
(cd "$root_dir/modules/jira-go" && CGO_ENABLED=0 GOOS="$target_os" GOARCH="$target_arch" "$go_command" build -trimpath -ldflags='-s -w' -o "bin/$platform/teamwork-jira" .)
(cd "$root_dir/modules/slack-go" && CGO_ENABLED=0 GOOS="$target_os" GOARCH="$target_arch" "$go_command" build -trimpath -ldflags='-s -w' -o "bin/$platform/teamwork-slack" .)
(cd "$root_dir/modules/onenote-go" && CGO_ENABLED=0 GOOS="$target_os" GOARCH="$target_arch" "$go_command" build -trimpath -ldflags='-s -w' -o "bin/$platform/teamwork-onenote" .)
(cd "$root_dir/modules/slack-onenote-go" && CGO_ENABLED=0 GOOS="$target_os" GOARCH="$target_arch" "$go_command" build -trimpath -ldflags='-s -w' -o "bin/$platform/teamwork-slack-onenote" .)

echo "Built TeamWork core and modules for $platform"
