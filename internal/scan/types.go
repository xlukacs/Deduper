package scan

import (
	"os"
	"time"
)

// Phase identifies the active part of a scan.
type Phase int

const (
	PhaseDiscovering Phase = iota
	PhaseHashing
)

// Progress is a point-in-time snapshot of scan activity.
type Progress struct {
	Phase              Phase
	CurrentPath        string
	DirectoriesVisited int64
	FilesCompleted     int64
	FilesTotal         int64
	BytesCompleted     int64
	BytesTotal         int64
	MatchesFound       int64
}

// Warning describes a recoverable filesystem problem.
type Warning struct {
	Path string
	Err  error
}

// FileRecord describes a successfully hashed regular file.
type FileRecord struct {
	Path string
	Size int64
	Hash [32]byte
	Info os.FileInfo
}

// DuplicateGroup contains distinct physical files with the same content hash.
type DuplicateGroup struct {
	Hash             [32]byte
	Size             int64
	Files            []FileRecord
	ReclaimableBytes int64
}

// Stats summarizes filesystem work.
type Stats struct {
	FilesDiscovered int64
	BytesDiscovered int64
	FilesHashed     int64
	BytesHashed     int64
	FilesSkipped    int64
	Errors          int64
}

// Result is the complete, read-only outcome of a scan.
type Result struct {
	Root       string
	Groups     []DuplicateGroup
	Stats      Stats
	Warnings   []Warning
	StartedAt  time.Time
	FinishedAt time.Time
}

// CleanupFolder describes a matching directory. Its contents are not inspected
// during cleanup search.
type CleanupFolder struct {
	Name string
	Path string
}

// CleanupResult is the read-only outcome of a cleanup folder search.
type CleanupResult struct {
	Root       string
	Names      []string
	Folders    []CleanupFolder
	Warnings   []Warning
	StartedAt  time.Time
	FinishedAt time.Time
}

// CleanupDeleteProgress reports recursive deletion activity.
type CleanupDeleteProgress struct {
	CurrentPath      string
	FoldersCompleted int
	FoldersTotal     int
	EntriesRemoved   int64
}

// Observer receives synchronous scan events. Implementations must return quickly.
type Observer interface {
	OnProgress(Progress)
	OnWarning(Warning)
}

type nopObserver struct{}

func (nopObserver) OnProgress(Progress) {}
func (nopObserver) OnWarning(Warning)   {}
