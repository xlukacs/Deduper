#!/usr/bin/env bash
set -euo pipefail
umask 022

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
project_root="$(cd -- "${script_dir}/.." && pwd)"
go_bin="${GO_BIN:-}"
version="${VERSION:-0.3.0}"

if [[ -z "${go_bin}" ]]; then
	if command -v go >/dev/null 2>&1; then
		go_bin="go"
	elif [[ -x /usr/local/go/bin/go ]]; then
		go_bin="/usr/local/go/bin/go"
	else
		echo "build-deb: Go was not found; install Go or set GO_BIN to the Go executable" >&2
		exit 1
	fi
elif ! command -v "${go_bin}" >/dev/null 2>&1; then
	echo "build-deb: GO_BIN does not point to a Go executable: ${go_bin}" >&2
	exit 1
fi
if ! command -v dpkg-deb >/dev/null 2>&1; then
	echo "build-deb: dpkg-deb was not found; install the dpkg-dev package" >&2
	exit 1
fi
if [[ ! "${version}" =~ ^[0-9][0-9A-Za-z.+:~_-]*$ ]]; then
	echo "build-deb: invalid Debian package version: ${version}" >&2
	exit 1
fi

if [[ -n "${DEB_ARCH:-}" ]]; then
	deb_arch="${DEB_ARCH}"
else
	case "$("${go_bin}" env GOARCH)" in
		amd64) deb_arch="amd64" ;;
		arm64) deb_arch="arm64" ;;
		386) deb_arch="i386" ;;
		arm) deb_arch="armhf" ;;
		*)
			echo "build-deb: unsupported host architecture; set DEB_ARCH explicitly" >&2
			exit 1
			;;
	esac
fi

case "${deb_arch}" in
	amd64)
		go_arch="amd64"
		go_arm=""
		;;
	arm64)
		go_arch="arm64"
		go_arm=""
		;;
	i386)
		go_arch="386"
		go_arm=""
		;;
	armhf)
		go_arch="arm"
		go_arm="7"
		;;
	*)
		echo "build-deb: unsupported Debian architecture: ${deb_arch}" >&2
		exit 1
		;;
esac

build_dir="$(mktemp -d)"
trap 'rm -rf -- "${build_dir}"' EXIT
package_root="${build_dir}/deduper_${version}_${deb_arch}"
output_dir="${project_root}/dist"
output_path="${output_dir}/deduper_${version}_${deb_arch}.deb"

install -d -m 0755 "${package_root}/DEBIAN" "${package_root}/usr/bin" \
	"${package_root}/usr/share/doc/deduper" "${output_dir}"

build_env=(env CGO_ENABLED=0 GOOS=linux GOARCH="${go_arch}")
if [[ -n "${go_arm}" ]]; then
	build_env+=(GOARM="${go_arm}")
fi
cd "${project_root}"
"${build_env[@]}" "${go_bin}" build -buildvcs=false -trimpath \
	-ldflags "-s -w -X main.version=${version}" \
	-o "${package_root}/usr/bin/deduper" ./cmd/deduper
chmod 0755 "${package_root}/usr/bin/deduper"

install -m 0644 "${project_root}/README.md" \
	"${package_root}/usr/share/doc/deduper/README.md"
install -m 0644 "${project_root}/packaging/debian/copyright" \
	"${package_root}/usr/share/doc/deduper/copyright"
install -m 0644 "${project_root}/LICENSE" \
	"${package_root}/usr/share/doc/deduper/LICENSE"

installed_size="$(du -sk "${package_root}/usr" | cut -f1)"
sed \
	-e "s/@VERSION@/${version}/g" \
	-e "s/@ARCHITECTURE@/${deb_arch}/g" \
	-e "s/@INSTALLED_SIZE@/${installed_size}/g" \
	"${project_root}/packaging/debian/control.in" >"${package_root}/DEBIAN/control"

chmod 0755 "${package_root}/DEBIAN"
chmod 0644 "${package_root}/DEBIAN/control"
dpkg-deb --root-owner-group --build "${package_root}" "${output_path}"
echo "Built ${output_path}"
