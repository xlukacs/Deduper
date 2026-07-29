package scan

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

type discoveredFile struct {
	path string
	size int64
}

var ignoredDirNames = map[string]struct{}{
	"node_modules": {},
	"venv":         {},
}

// Scan discovers and hashes all regular files below root.
func Scan(ctx context.Context, root string, observer Observer) (Result, error) {
	started := time.Now()
	result := Result{StartedAt: started}
	if observer == nil {
		observer = nopObserver{}
	}

	absRoot, err := validateRoot(root)
	if err != nil {
		return result, err
	}
	result.Root = absRoot

	warn := func(path string, err error) {
		warning := Warning{Path: path, Err: err}
		result.Warnings = append(result.Warnings, warning)
		result.Stats.Errors++
		observer.OnWarning(warning)
	}

	var discovered []discoveredFile
	err = filepath.WalkDir(absRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			if path == absRoot {
				return walkErr
			}
			warn(path, walkErr)
			result.Stats.FilesSkipped++
			if entry != nil && entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() && path != absRoot && isIgnoredDir(entry.Name()) {
			return filepath.SkipDir
		}

		observer.OnProgress(Progress{
			Phase:          PhaseDiscovering,
			CurrentPath:    path,
			FilesCompleted: result.Stats.FilesDiscovered,
			BytesCompleted: result.Stats.BytesDiscovered,
		})

		if entry.Type()&os.ModeSymlink != 0 {
			result.Stats.FilesSkipped++
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			warn(path, infoErr)
			result.Stats.FilesSkipped++
			return nil
		}
		if !info.Mode().IsRegular() {
			result.Stats.FilesSkipped++
			return nil
		}
		discovered = append(discovered, discoveredFile{path: path, size: info.Size()})
		result.Stats.FilesDiscovered++
		result.Stats.BytesDiscovered += info.Size()
		return nil
	})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return Result{}, err
		}
		return result, fmt.Errorf("walk %q: %w", absRoot, err)
	}

	candidates := duplicateSizeCandidates(discovered)
	var bytesToHash int64
	for _, item := range candidates {
		bytesToHash += item.size
	}

	files := make([]FileRecord, 0, len(candidates))
	var completedBytes int64
	for _, item := range candidates {
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}

		progress := Progress{
			Phase:          PhaseHashing,
			CurrentPath:    item.path,
			FilesCompleted: result.Stats.FilesHashed,
			FilesTotal:     int64(len(candidates)),
			BytesCompleted: completedBytes,
			BytesTotal:     bytesToHash,
		}
		observer.OnProgress(progress)

		record, bytesRead, hashErr := hashFile(ctx, item.path, progress, observer)
		if hashErr != nil {
			if errors.Is(hashErr, context.Canceled) || errors.Is(hashErr, context.DeadlineExceeded) {
				return Result{}, hashErr
			}
			warn(item.path, hashErr)
			result.Stats.FilesSkipped++
			completedBytes += bytesRead
			continue
		}
		files = append(files, record)
		result.Stats.FilesHashed++
		result.Stats.BytesHashed += record.Size
		completedBytes += record.Size
		observer.OnProgress(Progress{
			Phase:          PhaseHashing,
			CurrentPath:    item.path,
			FilesCompleted: result.Stats.FilesHashed,
			FilesTotal:     int64(len(candidates)),
			BytesCompleted: completedBytes,
			BytesTotal:     bytesToHash,
		})
	}

	result.Groups = FindDuplicates(files)
	result.FinishedAt = time.Now()
	return result, nil
}

func duplicateSizeCandidates(discovered []discoveredFile) []discoveredFile {
	counts := make(map[int64]int, len(discovered))
	for _, item := range discovered {
		counts[item.size]++
	}

	candidates := make([]discoveredFile, 0, len(discovered))
	for _, item := range discovered {
		if counts[item.size] > 1 {
			candidates = append(candidates, item)
		}
	}
	return candidates
}

func isIgnoredDir(name string) bool {
	_, ignored := ignoredDirNames[name]
	return ignored
}

func validateRoot(root string) (string, error) {
	if root == "" {
		return "", errors.New("scan root is required")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve root: %w", err)
	}
	info, err := os.Lstat(abs)
	if err != nil {
		return "", fmt.Errorf("inspect root %q: %w", abs, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("scan root %q is a symbolic link", abs)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("scan root %q is not a directory", abs)
	}
	return filepath.Clean(abs), nil
}

func hashFile(ctx context.Context, path string, base Progress, observer Observer) (FileRecord, int64, error) {
	before, err := os.Lstat(path)
	if err != nil {
		return FileRecord{}, 0, err
	}
	if !before.Mode().IsRegular() {
		return FileRecord{}, 0, errors.New("file is no longer a regular file")
	}

	f, err := os.Open(path)
	if err != nil {
		return FileRecord{}, 0, err
	}
	defer f.Close()

	h := sha256.New()
	buffer := make([]byte, 1024*1024)
	var read int64
	for {
		if err := ctx.Err(); err != nil {
			return FileRecord{}, read, err
		}
		n, readErr := f.Read(buffer)
		if n > 0 {
			if _, err := h.Write(buffer[:n]); err != nil {
				return FileRecord{}, read, err
			}
			read += int64(n)
			update := base
			update.BytesCompleted += read
			observer.OnProgress(update)
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return FileRecord{}, read, readErr
		}
	}

	after, err := f.Stat()
	if err != nil {
		return FileRecord{}, read, err
	}
	if before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) || read != after.Size() {
		return FileRecord{}, read, errors.New("file changed while being hashed")
	}
	var sum [32]byte
	copy(sum[:], h.Sum(nil))
	return FileRecord{Path: path, Size: after.Size(), Hash: sum, Info: after}, read, nil
}
