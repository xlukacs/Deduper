package scan

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/xlukacs/Deduper/internal/settings"
)

// FindCleanupFolders recursively finds directories whose exact names are in
// names. It records and prunes a matching directory without walking its
// contents. Folders excluded in options are never searched or matched. Hidden
// folders are matched by exact name but, when options ignores them, are not
// searched.
func FindCleanupFolders(ctx context.Context, root string, names []string, options Options, observer Observer) (CleanupResult, error) {
	started := time.Now()
	result := CleanupResult{StartedAt: started}
	if observer == nil {
		observer = nopObserver{}
	}

	absRoot, err := validateRoot(root)
	if err != nil {
		return result, err
	}
	result.Root = absRoot
	matchingNames := make(map[string]struct{}, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if !settings.ValidCleanupName(name) {
			continue
		}
		matchingNames[name] = struct{}{}
	}
	for name := range matchingNames {
		result.Names = append(result.Names, name)
	}
	sort.Strings(result.Names)
	if len(matchingNames) == 0 {
		result.FinishedAt = time.Now()
		return result, nil
	}

	warn := func(path string, err error) {
		warning := Warning{Path: path, Err: err}
		result.Warnings = append(result.Warnings, warning)
		observer.OnWarning(warning)
	}
	ignored := newExclusionRules(options, nil)
	var visited, matchesFound int64
	err = filepath.WalkDir(absRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			if path == absRoot {
				return walkErr
			}
			warn(path, walkErr)
			if entry != nil && entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		// WalkDir also reports files so it can descend through their parent
		// directories. Cleanup matches only directory entries and never opens files.
		if !entry.IsDir() || path == absRoot {
			return nil
		}
		relative, relErr := filepath.Rel(absRoot, path)
		if relErr != nil {
			return relErr
		}
		if ignored.excluded(relative, entry.Name()) {
			return filepath.SkipDir
		}

		visited++
		_, matches := matchingNames[entry.Name()]
		if matches {
			matchesFound++
			result.Folders = append(result.Folders, CleanupFolder{Name: entry.Name(), Path: path})
		}
		observer.OnProgress(Progress{
			Phase:              PhaseDiscovering,
			CurrentPath:        path,
			DirectoriesVisited: visited,
			MatchesFound:       matchesFound,
		})
		if matches || ignored.hidden(entry.Name()) {
			return filepath.SkipDir
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return CleanupResult{}, err
		}
		return result, fmt.Errorf("walk %q: %w", absRoot, err)
	}
	sort.Slice(result.Folders, func(i, j int) bool { return result.Folders[i].Path < result.Folders[j].Path })
	result.FinishedAt = time.Now()
	return result, nil
}

// DeleteCleanupFolders removes the selected folders beneath root and reports
// each entry removed. Paths are checked for containment and operated on through
// os.Root so symlinks cannot make a target escape the selected root. Folders
// that are not in the returned removed list could not be fully deleted.
func DeleteCleanupFolders(ctx context.Context, root string, folders []CleanupFolder, onProgress func(CleanupDeleteProgress)) ([]string, []Warning, error) {
	absRoot, err := validateRoot(root)
	if err != nil {
		return nil, nil, err
	}
	rootHandle, err := os.OpenRoot(absRoot)
	if err != nil {
		return nil, nil, fmt.Errorf("open cleanup root: %w", err)
	}
	defer rootHandle.Close()

	removed := make([]string, 0, len(folders))
	warnings := make([]Warning, 0)
	ordered := append([]CleanupFolder(nil), folders...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Path < ordered[j].Path })
	progress := CleanupDeleteProgress{FoldersTotal: len(ordered)}
	emitProgress := func() {
		if onProgress != nil {
			onProgress(progress)
		}
	}
	for _, folder := range ordered {
		if err := ctx.Err(); err != nil {
			return removed, warnings, err
		}
		progress.CurrentPath = folder.Path
		emitProgress()
		ok, err := deleteCleanupFolder(ctx, rootHandle, absRoot, folder, &progress, emitProgress, &warnings)
		if err != nil {
			return removed, warnings, err
		}
		if ok {
			removed = append(removed, folder.Path)
		}
		progress.FoldersCompleted++
		emitProgress()
	}
	return removed, warnings, nil
}

// deleteCleanupFolder removes one matched folder. It records recoverable
// problems as warnings and returns an error only when ctx is cancelled.
func deleteCleanupFolder(ctx context.Context, rootHandle *os.Root, absRoot string, folder CleanupFolder, progress *CleanupDeleteProgress, emitProgress func(), warnings *[]Warning) (bool, error) {
	fail := func(err error) (bool, error) {
		*warnings = append(*warnings, Warning{Path: folder.Path, Err: err})
		return false, nil
	}
	relative, err := filepath.Rel(absRoot, filepath.Clean(folder.Path))
	if err != nil {
		return fail(err)
	}
	if relative == "." || filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fail(errors.New("cleanup folder is outside the selected root"))
	}
	if folder.Name != filepath.Base(relative) {
		return fail(errors.New("cleanup folder name does not match its path"))
	}
	if parent := filepath.Dir(relative); parent != "." {
		components := strings.Split(parent, string(filepath.Separator))
		for i := range components {
			parentInfo, err := rootHandle.Lstat(filepath.Join(components[:i+1]...))
			if err != nil {
				return fail(err)
			}
			if !parentInfo.IsDir() || parentInfo.Mode()&os.ModeSymlink != 0 {
				return fail(errors.New("cleanup path contains a non-directory or symbolic-link parent"))
			}
		}
	}

	info, err := rootHandle.Lstat(relative)
	if err != nil {
		return fail(err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fail(errors.New("cleanup target is no longer a directory"))
	}
	targetRoot, err := rootHandle.OpenRoot(relative)
	if err != nil {
		return fail(err)
	}
	if err := removeCleanupTree(ctx, targetRoot, folder.Path, progress, emitProgress, warnings); err != nil {
		targetRoot.Close()
		return false, err
	}
	if err := targetRoot.Close(); err != nil {
		*warnings = append(*warnings, Warning{Path: folder.Path, Err: err})
	}
	finalInfo, err := rootHandle.Lstat(relative)
	if err != nil {
		return fail(err)
	}
	if !finalInfo.IsDir() || finalInfo.Mode()&os.ModeSymlink != 0 || !os.SameFile(info, finalInfo) {
		return fail(errors.New("cleanup target changed during deletion"))
	}
	if err := rootHandle.Remove(relative); err != nil {
		return fail(err)
	}
	progress.EntriesRemoved++
	return true, nil
}

func removeCleanupTree(ctx context.Context, dir *os.Root, path string, progress *CleanupDeleteProgress, emitProgress func(), warnings *[]Warning) error {
	listing, err := dir.Open(".")
	if err != nil {
		*warnings = append(*warnings, Warning{Path: path, Err: err})
		return nil
	}
	entries, readErr := listing.ReadDir(-1)
	closeErr := listing.Close()
	if readErr != nil {
		*warnings = append(*warnings, Warning{Path: path, Err: readErr})
	}
	if closeErr != nil {
		*warnings = append(*warnings, Warning{Path: path, Err: closeErr})
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		entryPath := filepath.Join(path, entry.Name())
		progress.CurrentPath = entryPath
		emitProgress()
		if entry.IsDir() && entry.Type()&os.ModeSymlink == 0 {
			child, err := dir.OpenRoot(entry.Name())
			if err != nil {
				*warnings = append(*warnings, Warning{Path: entryPath, Err: err})
				continue
			}
			if err := removeCleanupTree(ctx, child, entryPath, progress, emitProgress, warnings); err != nil {
				child.Close()
				return err
			}
			if err := child.Close(); err != nil {
				*warnings = append(*warnings, Warning{Path: entryPath, Err: err})
			}
		}
		progress.CurrentPath = entryPath
		if err := dir.Remove(entry.Name()); err != nil {
			*warnings = append(*warnings, Warning{Path: entryPath, Err: err})
			continue
		}
		progress.EntriesRemoved++
		emitProgress()
	}
	return nil
}
