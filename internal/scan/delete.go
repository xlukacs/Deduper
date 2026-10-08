package scan

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// DeleteDuplicates removes only selected files from the supplied scan groups.
// A verified, unselected physical copy must survive each group. Files are
// checked against their scan identity and hash before removal through os.Root.
func DeleteDuplicates(ctx context.Context, root string, groups []DuplicateGroup, selected map[string]bool) ([]string, []Warning, error) {
	absRoot, err := validateRoot(root)
	if err != nil {
		return nil, nil, err
	}
	dir, err := os.OpenRoot(absRoot)
	if err != nil {
		return nil, nil, fmt.Errorf("open deletion root: %w", err)
	}
	defer dir.Close()
	var removed []string
	var warnings []Warning
	known := make(map[string]bool)
	for _, group := range groups {
		var targets, keepers []FileRecord
		for _, file := range group.Files {
			known[file.Path] = true
			if selected[file.Path] {
				targets = append(targets, file)
			} else {
				keepers = append(keepers, file)
			}
		}
		if len(targets) == 0 {
			continue
		}
		var keeper FileRecord
		keeperRelative := ""
		for _, file := range keepers {
			if file.Hash != group.Hash || file.Size != group.Size {
				continue
			}
			relative, verifyErr := verifyDuplicate(ctx, dir, absRoot, file)
			if err := ctx.Err(); err != nil {
				return removed, warnings, err
			}
			if verifyErr == nil {
				keeper, keeperRelative = file, relative
				break
			}
		}
		for _, file := range targets {
			if err := ctx.Err(); err != nil {
				return removed, warnings, err
			}
			var deleteErr error
			switch {
			case keeperRelative == "":
				deleteErr = errors.New("no unchanged, unflagged copy remains; rescan before deleting")
			case file.Hash != group.Hash || file.Size != group.Size:
				deleteErr = errors.New("file does not match its duplicate group")
			case file.Info != nil && os.SameFile(file.Info, keeper.Info):
				deleteErr = errors.New("remaining copy is a hard link to this file")
			default:
				var relative string
				relative, deleteErr = verifyDuplicate(ctx, dir, absRoot, file)
				if deleteErr == nil {
					deleteErr = checkDuplicateIdentity(dir, keeperRelative, keeper)
				}
				if deleteErr == nil {
					deleteErr = checkDuplicateIdentity(dir, relative, file)
				}
				if deleteErr == nil {
					deleteErr = ctx.Err()
				}
				if deleteErr == nil {
					deleteErr = dir.Remove(relative)
				}
			}
			if err := ctx.Err(); err != nil && deleteErr != nil {
				return removed, warnings, err
			}
			if deleteErr != nil {
				warnings = append(warnings, Warning{Path: file.Path, Err: deleteErr})
			} else {
				removed = append(removed, file.Path)
			}
		}
	}
	for path, marked := range selected {
		if marked && !known[path] {
			warnings = append(warnings, Warning{Path: path, Err: errors.New("file is not in the scan results")})
		}
	}
	return removed, warnings, nil
}

func checkDuplicateIdentity(dir *os.Root, relative string, record FileRecord) error {
	parent := ""
	components := strings.Split(relative, string(filepath.Separator))
	for _, component := range components[:len(components)-1] {
		parent = filepath.Join(parent, component)
		info, err := dir.Lstat(parent)
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("file path contains a non-directory or symbolic-link parent")
		}
	}
	info, err := dir.Lstat(relative)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || record.Info == nil || !os.SameFile(info, record.Info) ||
		info.Size() != record.Size || !info.ModTime().Equal(record.Info.ModTime()) {
		return errors.New("file changed since the scan; rescan before deleting")
	}
	return nil
}

func verifyDuplicate(ctx context.Context, dir *os.Root, absRoot string, record FileRecord) (string, error) {
	relative, err := filepath.Rel(absRoot, record.Path)
	if err != nil || relative == "." || !filepath.IsLocal(relative) {
		return "", errors.New("file is outside the scan root")
	}
	if err := checkDuplicateIdentity(dir, relative, record); err != nil {
		return "", err
	}
	f, err := dir.Open(relative)
	if err != nil {
		return "", err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	if !os.SameFile(info, record.Info) {
		return "", errors.New("file changed while opening it")
	}
	hash := sha256.New()
	buffer := make([]byte, 128*1024)
	var read int64
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		n, readErr := f.Read(buffer)
		if n > 0 {
			hash.Write(buffer[:n])
			read += int64(n)
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return "", readErr
		}
	}
	var sum [32]byte
	copy(sum[:], hash.Sum(nil))
	if read != record.Size || sum != record.Hash {
		return "", errors.New("file contents changed since the scan; rescan before deleting")
	}
	if err := checkDuplicateIdentity(dir, relative, record); err != nil {
		return "", err
	}
	return relative, nil
}
