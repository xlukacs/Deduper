# Deduper

Deduper is a terminal application with separate duplicate-finding and folder
cleanup modes. Duplicate scans are read-only: they find files with identical
content, collapse hard-linked paths, and report the storage used by extra copies.
After an interactive scan, you can flag individual duplicate files and confirm
their permanent deletion as a batch.

## Build

Go 1.25 or newer is required.

```sh
go build -o deduper ./cmd/deduper
```

## Install on Ubuntu

Build an Ubuntu/Debian package for the current machine:

```sh
./scripts/build-deb.sh
sudo apt install ./dist/deduper_0.2.1_amd64.deb
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

Searching for duplicates and non-interactive scans never modify files. In
interactive duplicate results, files are deleted only after you flag them and
confirm the batch. Cleanup mode searches for configured directory names, lists
matching paths, and removes a match only after you confirm. The search stops at
each matching directory; deletion removes that directory and everything inside
it. Symbolic links are not followed.
Recoverable filesystem errors are reported as warnings, and scanning continues.

Duplicate scans ignore directories named `venv` or `node_modules`. Deduper also
avoids hashing files whose byte size is unique within the scan, because they
cannot be content duplicates. This keeps exact duplicate detection while
avoiding needless reads of large unique files on remote or slow storage.

### Deleting duplicate files

After a duplicate search finishes, press `Tab` to focus **Matching files**. Use
arrows or `j`/`k` to select a file, then press `d` to flag it for deletion.
Press `d` again to unflag it. Flagged files show `[DELETE]`; marks are kept while
filtering or sorting, so you can work through multiple groups before deleting.
At least one copy in each group must remain unflagged.

Press `Enter` to review every flagged path and the total size. Use arrows or
`j`/`k` to scroll the review, then press `y` to permanently erase the batch.
Press `n` or `Esc` to cancel and keep the marks, or `u` to discard all marks.
Quitting, choosing a new folder, or rescanning with pending marks also opens
this review; after deletion or discarding the marks, the requested action
continues. `Ctrl+C` quits immediately.

Before deletion, Deduper checks file identities and contents again and verifies
an unflagged copy remains. Changed, replaced, or missing files are skipped and
reported under warnings. Failed deletions keep their marks. Press `Esc` during
deletion to stop further removals. Results update to show the copies remaining.

### Cleanup mode

On the first TUI screen, enter a root folder and press `Enter`. Choose **Clean up
matching folders** on the next screen. By default, cleanup mode finds exact
directory names `venv`, `.venv`, `node_modules`, `__pycache__`, `.pytest_cache`,
and `.mypy_cache` anywhere below the selected root. Search checks directory
names only and skips the contents of a match. The results screen lists each
matching path; press `d`, then `y`, to remove every listed folder and its
contents. Choose **Edit cleanup folder names** on the mode screen, or press `m`
in cleanup results, to add, rename, or remove names. Rules match directory
names exactly at any depth and are saved in settings. Changing the names from
cleanup results starts a new search, so only folders that match the current
names can be deleted.

Cleanup search respects your folder exclusions: it never searches or matches an
excluded folder. With hidden-folder exclusion on, it still matches hidden names
you list (such as `.venv`) but does not search inside other hidden folders.

Cleanup search shows the current directory, how many directories it has checked,
and how many matches it has found. Deletion shows the current path, completed
folders, and entries removed. Press `Esc` during deletion to stop it.

### Excluding folders

Press `o` in the interactive application to open scan settings. There you can
toggle hidden-folder exclusions (such as `.git`, `.turbo`, and `.ssh`) and add
or remove folder exclusions. A bare folder name applies anywhere below the scan
root; a path with `/` applies relative to the scan root. Settings are saved for
future interactive and plain-text scans. Duplicate scans also always skip
`node_modules` and `venv`; cleanup mode does not apply that built-in rule.

### Hash workers

Hashing runs concurrently, using the number of CPUs configured for Go by
default. Press `o` in the interactive application to choose the number of
files hashed at once. For a one-off non-interactive scan, `DEDUPER_WORKERS`
remains available as an environment override.

## Interactive keys

- `Enter` on the folder screen opens the mode choices. On that screen, use
  arrows or `j`/`k` to choose a mode and press `Enter`.
- `Tab` switches focus.
- Arrow keys or `j`/`k` navigate results.
- `/` filters duplicate groups by path.
- `s` cycles sorting by reclaimable space, biggest file, most copies, most
  recently updated copy, and path.
- In duplicate results, `d` toggles a deletion flag on the selected file in
  **Matching files**, `Enter` reviews the batch, `y` confirms deletion, and `u`
  clears the flags.
- `r` rescans, `n` chooses another folder, and `w` shows warnings. In cleanup
  results, `m` edits folder names and `d`, then `y`, deletes every match.
- `Esc` returns to folder selection, cancels an active search, or stops deletion.
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
