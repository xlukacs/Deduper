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
)

// FindCleanupFolders recursively finds directories whose exact names are in
// names. It records and prunes a matching directory without walking its
// contents.
func FindCleanupFolders(ctx context.Context, root string, names []string, observer Observer) (CleanupResult, error) {
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
		if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\\`) {
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
		if matches {
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
// os.Root so symlinks cannot make a target escape the selected root.
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
	removedRelative := make([]string, 0, len(ordered))
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
		cleanPath := filepath.Clean(folder.Path)
		relative, err := filepath.Rel(absRoot, cleanPath)
		if err != nil || relative == "." || filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			if err == nil {
				err = errors.New("cleanup folder is outside the selected root")
			}
			warnings = append(warnings, Warning{Path: folder.Path, Err: err})
			progress.FoldersCompleted++
			emitProgress()
			continue
		}
		insideRemovedFolder := false
		for _, parent := range removedRelative {
			if strings.HasPrefix(relative, parent+string(filepath.Separator)) {
				insideRemovedFolder = true
				break
			}
		}
		if insideRemovedFolder {
			removed = append(removed, folder.Path)
			progress.FoldersCompleted++
			emitProgress()
			continue
		}
		if folder.Name != filepath.Base(relative) {
			warnings = append(warnings, Warning{Path: folder.Path, Err: errors.New("cleanup folder name does not match its path")})
			progress.FoldersCompleted++
			emitProgress()
			continue
		}
		components := strings.Split(relative, string(filepath.Separator))
		parent := ""
		parentInvalid := false
		for _, component := range components[:len(components)-1] {
			if parent == "" {
				parent = component
			} else {
				parent = filepath.Join(parent, component)
			}
			parentInfo, parentErr := rootHandle.Lstat(parent)
			if parentErr != nil {
				warnings = append(warnings, Warning{Path: folder.Path, Err: parentErr})
				parentInvalid = true
				break
			}
			if !parentInfo.IsDir() || parentInfo.Mode()&os.ModeSymlink != 0 {
				warnings = append(warnings, Warning{Path: folder.Path, Err: errors.New("cleanup path contains a non-directory or symbolic-link parent")})
				parentInvalid = true
				break
			}
		}
		if parentInvalid {
			progress.FoldersCompleted++
			emitProgress()
			continue
		}

		info, err := rootHandle.Lstat(relative)
		if err != nil {
			warnings = append(warnings, Warning{Path: folder.Path, Err: err})
			progress.FoldersCompleted++
			emitProgress()
			continue
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			warnings = append(warnings, Warning{Path: folder.Path, Err: errors.New("cleanup target is no longer a directory")})
			progress.FoldersCompleted++
			emitProgress()
			continue
		}
		targetRoot, err := rootHandle.OpenRoot(relative)
		if err != nil {
			warnings = append(warnings, Warning{Path: folder.Path, Err: err})
			progress.FoldersCompleted++
			emitProgress()
			continue
		}
		if err := removeCleanupTree(ctx, targetRoot, folder.Path, &progress, emitProgress, &warnings); err != nil {
			targetRoot.Close()
			return removed, warnings, err
		}
		if err := targetRoot.Close(); err != nil {
			warnings = append(warnings, Warning{Path: folder.Path, Err: err})
		}
		finalInfo, err := rootHandle.Lstat(relative)
		if err != nil {
			warnings = append(warnings, Warning{Path: folder.Path, Err: err})
			progress.FoldersCompleted++
			emitProgress()
			continue
		}
		if !finalInfo.IsDir() || finalInfo.Mode()&os.ModeSymlink != 0 || !os.SameFile(info, finalInfo) {
			warnings = append(warnings, Warning{Path: folder.Path, Err: errors.New("cleanup target changed during deletion")})
			progress.FoldersCompleted++
			emitProgress()
			continue
		}
		if err := rootHandle.Remove(relative); err != nil {
			warnings = append(warnings, Warning{Path: folder.Path, Err: err})
			progress.FoldersCompleted++
			emitProgress()
			continue
		}
		progress.EntriesRemoved++
		removed = append(removed, folder.Path)
		removedRelative = append(removedRelative, relative)
		progress.FoldersCompleted++
		emitProgress()
	}
	return removed, warnings, nil
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
