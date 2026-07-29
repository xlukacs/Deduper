package report

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/xlukacs/Deduper/internal/scan"
)

func TestTextNoDuplicates(t *testing.T) {
	result := scan.Result{StartedAt: time.Unix(0, 0), FinishedAt: time.Unix(1, 0)}
	var out bytes.Buffer
	if err := Text(&out, result); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "No duplicates found.") || !strings.Contains(out.String(), "Elapsed: 1s") {
		t.Fatalf("unexpected report:\n%s", out.String())
	}
}

func TestBytes(t *testing.T) {
	for value, want := range map[int64]string{0: "0 B", 1024: "1.0 KiB", 1048576: "1.0 MiB"} {
		if got := Bytes(value); got != want {
			t.Errorf("Bytes(%d) = %q, want %q", value, got, want)
		}
	}
}
