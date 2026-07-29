#!/usr/bin/env bash
set -euo pipefail
umask 022

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
project_root="$(cd -- "${script_dir}/.." && pwd)"
go_bin="${GO_BIN:-}"
version="${VERSION:-}"
all_targets=false

usage() {
	printf '%s\n' "Usage: build-npm.sh [--all] [--version VERSION]"
}

while [[ $# -gt 0 ]]; do
	case "$1" in
		--all)
			all_targets=true
			;;
		--version)
			shift
			if [[ $# -eq 0 ]]; then
				echo "build-npm: --version needs a value" >&2
				exit 1
			fi
			version="$1"
			;;
		--help|-h)
			usage
			exit 0
			;;
		*)
			usage >&2
			exit 1
			;;
	esac
	shift
done

if [[ -z "${go_bin}" ]]; then
	if command -v go >/dev/null 2>&1; then
		go_bin="go"
	elif [[ -x /usr/local/go/bin/go ]]; then
		go_bin="/usr/local/go/bin/go"
	else
		echo "build-npm: Go was not found; install Go or set GO_BIN to the Go executable" >&2
		exit 1
	fi
elif ! command -v "${go_bin}" >/dev/null 2>&1; then
	echo "build-npm: GO_BIN does not point to a Go executable: ${go_bin}" >&2
	exit 1
fi
if [[ -z "${version}" ]]; then
	if ! command -v node >/dev/null 2>&1; then
		echo "build-npm: Node.js was not found; pass --version explicitly" >&2
		exit 1
	fi
	version="$(node -p "require('${project_root}/package.json').version")"
fi
if [[ ! "${version}" =~ ^[0-9]+\.[0-9]+\.[0-9]+([-.+][0-9A-Za-z.-]+)?$ ]]; then
	echo "build-npm: invalid npm package version: ${version}" >&2
	exit 1
fi

if "${all_targets}"; then
	targets=(darwin/amd64 darwin/arm64 linux/amd64 linux/arm64 windows/amd64)
else
	targets=("$("${go_bin}" env GOOS)/$("${go_bin}" env GOARCH)")
fi

output_dir="${project_root}/npm/bin"
install -d -m 0755 "${output_dir}"
cd "${project_root}"
for target in "${targets[@]}"; do
	case "${target}" in
		darwin/amd64) binary="deduper-darwin-x64" ;;
		darwin/arm64) binary="deduper-darwin-arm64" ;;
		linux/amd64) binary="deduper-linux-x64" ;;
		linux/arm64) binary="deduper-linux-arm64" ;;
		windows/amd64) binary="deduper-win32-x64.exe" ;;
		*)
			echo "build-npm: unsupported target: ${target}" >&2
			exit 1
			;;
	esac

	goos="${target%%/*}"
	goarch="${target##*/}"
	env CGO_ENABLED=0 GOOS="${goos}" GOARCH="${goarch}" "${go_bin}" build \
		-buildvcs=false -trimpath -ldflags "-s -w -X main.version=${version}" \
		-o "${output_dir}/${binary}" ./cmd/deduper
	if [[ "${goos}" != "windows" ]]; then
		chmod 0755 "${output_dir}/${binary}"
	fi
done

printf 'Built npm binaries for %s\n' "${targets[*]}"
