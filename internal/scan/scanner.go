package scan

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

type discoveredFile struct {
	path string
	size int64
}

var defaultIgnoredDirNames = []string{"node_modules", "venv"}

// Options controls a scan's resource usage.
type Options struct {
	// Workers is the number of files hashed concurrently. A value of zero uses
	// DefaultOptions.
	Workers int
	// IgnoreHiddenFolders skips all folders whose names start with a dot.
	IgnoreHiddenFolders bool
	// ExcludedFolders contains either folder names (matched anywhere) or paths
	// relative to the scan root.
	ExcludedFolders []string
}

// DefaultOptions returns the options used by Scan. Set DEDUPER_WORKERS to a
// positive integer to override the default CPU-sized hash worker pool.
func DefaultOptions() Options {
	workers := runtime.GOMAXPROCS(0)
	if configured, ok := EnvironmentWorkers(); ok {
		workers = configured
	}
	return Options{Workers: workers}
}

// EnvironmentWorkers returns the positive DEDUPER_WORKERS value, if set.
func EnvironmentWorkers() (int, bool) {
	configured, err := strconv.Atoi(strings.TrimSpace(os.Getenv("DEDUPER_WORKERS")))
	return configured, err == nil && configured > 0
}

// Scan discovers and hashes all regular files below root.
func Scan(ctx context.Context, root string, observer Observer) (Result, error) {
	return ScanWithOptions(ctx, root, observer, DefaultOptions())
}

// ScanWithOptions discovers and hashes all regular files below root.
func ScanWithOptions(ctx context.Context, root string, observer Observer, options Options) (Result, error) {
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
	workers := options.Workers
	if workers <= 0 {
		workers = DefaultOptions().Workers
	}

	warn := func(path string, err error) {
		warning := Warning{Path: path, Err: err}
		result.Warnings = append(result.Warnings, warning)
		result.Stats.Errors++
		observer.OnWarning(warning)
	}
	ignored := newIgnoreRules(options)

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
		if entry.IsDir() && path != absRoot {
			relative, relErr := filepath.Rel(absRoot, path)
			if relErr != nil {
				return relErr
			}
			if ignored.matches(relative, entry.Name()) {
				return filepath.SkipDir
			}
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
	progress := func(path string) Progress {
		return Progress{
			Phase:          PhaseHashing,
			CurrentPath:    path,
			FilesCompleted: result.Stats.FilesHashed,
			FilesTotal:     int64(len(candidates)),
			BytesCompleted: completedBytes,
			BytesTotal:     bytesToHash,
		}
	}
	observer.OnProgress(progress(absRoot))

	events := make(chan hashEvent, workers*2)
	jobs := make(chan discoveredFile)
	var workerGroup sync.WaitGroup
	for range workers {
		workerGroup.Add(1)
		go func() {
			defer workerGroup.Done()
			for item := range jobs {
				if ctx.Err() != nil {
					return
				}
				record, _, hashErr := hashFile(ctx, item.path, func(bytesRead int64) error {
					select {
					case events <- hashEvent{path: item.path, bytesRead: bytesRead}:
						return nil
					case <-ctx.Done():
						return ctx.Err()
					}
				})
				select {
				case events <- hashEvent{path: item.path, record: record, err: hashErr, done: true}:
				case <-ctx.Done():
					return
				}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, item := range candidates {
			select {
			case jobs <- item:
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() {
		workerGroup.Wait()
		close(events)
	}()

	for event := range events {
		if !event.done {
			completedBytes += event.bytesRead
			observer.OnProgress(progress(event.path))
			continue
		}
		if event.err != nil {
			if !errors.Is(event.err, context.Canceled) && !errors.Is(event.err, context.DeadlineExceeded) {
				warn(event.path, event.err)
				result.Stats.FilesSkipped++
			}
			continue
		}
		files = append(files, event.record)
		result.Stats.FilesHashed++
		result.Stats.BytesHashed += event.record.Size
		observer.OnProgress(progress(event.path))
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
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

type ignoreRules struct {
	names              map[string]struct{}
	paths              map[string]struct{}
	ignoreHiddenFolder bool
}

func newIgnoreRules(options Options) ignoreRules {
	rules := ignoreRules{
		names:              make(map[string]struct{}, len(defaultIgnoredDirNames)),
		paths:              make(map[string]struct{}),
		ignoreHiddenFolder: options.IgnoreHiddenFolders,
	}
	for _, name := range defaultIgnoredDirNames {
		rules.names[name] = struct{}{}
	}
	for _, rawEntry := range options.ExcludedFolders {
		entry := strings.TrimSpace(rawEntry)
		if entry == "" {
			continue
		}
		entry = filepath.Clean(filepath.FromSlash(strings.ReplaceAll(entry, "\\", "/")))
		if entry == "." || entry == ".." || filepath.IsAbs(entry) || strings.HasPrefix(entry, ".."+string(filepath.Separator)) {
			continue
		}
		if strings.Contains(entry, string(filepath.Separator)) {
			rules.paths[entry] = struct{}{}
		} else {
			rules.names[entry] = struct{}{}
		}
	}
	return rules
}

func (rules ignoreRules) matches(relative, name string) bool {
	if rules.ignoreHiddenFolder && strings.HasPrefix(name, ".") {
		return true
	}
	if _, ignored := rules.names[name]; ignored {
		return true
	}
	_, ignored := rules.paths[filepath.Clean(relative)]
	return ignored
}

type hashEvent struct {
	path      string
	bytesRead int64
	record    FileRecord
	err       error
	done      bool
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

func hashFile(ctx context.Context, path string, onRead func(int64) error) (FileRecord, int64, error) {
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
			if err := onRead(int64(n)); err != nil {
				return FileRecord{}, read, err
			}
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
