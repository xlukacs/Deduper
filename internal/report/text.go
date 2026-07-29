// Package report renders non-interactive scan results.
package report

import (
	"fmt"
	"io"
	"time"

	"github.com/xlukacs/Deduper/internal/scan"
)

// Text writes a stable, human-readable report without terminal escape codes.
func Text(w io.Writer, result scan.Result) error {
	if len(result.Groups) == 0 {
		if _, err := fmt.Fprintln(w, "No duplicates found."); err != nil {
			return err
		}
	} else {
		for i, group := range result.Groups {
			if i > 0 {
				if _, err := fmt.Fprintln(w); err != nil {
					return err
				}
			}
			if _, err := fmt.Fprintf(w, "Duplicate group: %d files, %s each, %s reclaimable\n", len(group.Files), Bytes(group.Size), Bytes(group.ReclaimableBytes)); err != nil {
				return err
			}
			if _, err := fmt.Fprintf(w, "SHA-256: %x\n", group.Hash); err != nil {
				return err
			}
			for _, file := range group.Files {
				if _, err := fmt.Fprintf(w, "  %s\n", file.Path); err != nil {
					return err
				}
			}
		}
	}

	var copies int
	var reclaimable int64
	for _, group := range result.Groups {
		copies += len(group.Files) - 1
		reclaimable += group.ReclaimableBytes
	}
	duration := result.FinishedAt.Sub(result.StartedAt)
	if duration < 0 {
		duration = 0
	}
	_, err := fmt.Fprintf(w,
		"\nSummary\n  Files discovered: %d (%s)\n  Files hashed: %d (%s)\n  Duplicate groups: %d\n  Excess copies: %d\n  Reclaimable: %s\n  Skipped files: %d\n  Warnings: %d\n  Elapsed: %s\n",
		result.Stats.FilesDiscovered, Bytes(result.Stats.BytesDiscovered),
		result.Stats.FilesHashed, Bytes(result.Stats.BytesHashed), len(result.Groups), copies,
		Bytes(reclaimable), result.Stats.FilesSkipped, len(result.Warnings), duration.Round(time.Millisecond),
	)
	return err
}

// Bytes formats a byte count with binary units.
func Bytes(value int64) string {
	const unit = int64(1024)
	if value < unit {
		return fmt.Sprintf("%d B", value)
	}
	div, exp := unit, 0
	for n := value / unit; n >= unit && exp < 5; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(value)/float64(div), "KMGTPE"[exp])
}
