# Deduper

Deduper is a read-only terminal application that finds files with identical
content. It recursively discovers regular files, streams them through SHA-256,
collapses hard-linked paths, and reports the storage that duplicate copies use.

## Build

Go 1.25 or newer is required.

```sh
go build -o deduper ./cmd/deduper
```

## Install on Ubuntu

Build an Ubuntu/Debian package for the current machine:

```sh
./scripts/build-deb.sh
sudo apt install ./dist/deduper_0.2.0_amd64.deb
```

The package installs the `deduper` command in `/usr/bin`. Go is only required
to build the package; it is not required on the computer where the package is
installed. Set `VERSION` or `DEB_ARCH` when producing another release or a
cross-compiled `amd64`, `arm64`, `i386`, or `armhf` package.

## Install with npm

Install the cross-platform command-line package:

```sh
npm install --global @madrent/deduper
deduper
```

The npm package bundles native binaries for Linux (x64 and ARM64), macOS
(Intel and Apple Silicon), and Windows (x64). It does not require Go at
install time.

## Use

Open the full-screen application:

```sh
./deduper
```

Run a non-interactive scan:

```sh
./deduper scan /path/to/folder
```

The application never deletes, moves, links, or modifies files. Symbolic links
and non-regular files are skipped. Recoverable filesystem errors are reported as
warnings, and scanning continues.

Directories named `venv` or `node_modules` are ignored. Deduper also avoids
hashing files whose byte size is unique within the scan, because they cannot be
content duplicates. This keeps exact duplicate detection while avoiding needless
reads of large unique files on remote or slow storage.

### Excluding folders

Press `o` in the interactive application to open scan settings. There you can
toggle hidden-folder exclusions (such as `.git`, `.turbo`, and `.ssh`) and add
or remove folder exclusions. A bare folder name applies anywhere below the scan
root; a path with `/` applies relative to the scan root. Settings are saved for
future interactive and plain-text scans. `node_modules` and `venv` remain
excluded by default.

### Hash workers

Hashing runs concurrently, using the number of CPUs configured for Go by
default. Press `o` in the interactive application to choose the number of
files hashed at once. For a one-off non-interactive scan, `DEDUPER_WORKERS`
remains available as an environment override.

## Interactive keys

- `Enter` starts a scan.
- `Tab` switches focus.
- Arrow keys or `j`/`k` navigate results.
- `/` filters duplicate groups by path.
- `s` cycles sorting by reclaimable space, biggest file, most copies, most
  recently updated copy, and path.
- `r` rescans, `n` chooses another folder, and `w` shows warnings.
- `Esc` cancels an active scan.
- `Ctrl+C` quits from anywhere.

## Development

```sh
go test ./...
go test -race ./...
go vet ./...
```

Build the npm package binaries for the current platform or for every supported
platform:

```sh
npm run build:npm
npm run build:npm:all
```

### Publishing the npm package

The first release creates the package on npm. After committing and pushing the
release setup, run:

```sh
npm run build:npm:all
npm publish --access public
```

Then configure npm trusted publishing for GitHub Actions using the `xlukacs`
GitHub user, the `Deduper` repository, the `publish-npm.yml` workflow, and the
`npm` GitHub environment. Later releases are published by pushing tags such as
`v0.1.1`; they need no npm token. Before tagging a later release, update the
`version` in `package.json` and `cmd/deduper/main.go` to match the tag without
its leading `v`.
