package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHelpVersionAndUsage(t *testing.T) {
	for _, test := range []struct {
		args     []string
		code     int
		contains string
	}{
		{[]string{"help"}, 0, "Usage:"},
		{[]string{"version"}, 0, "deduper 0.3.0"},
		{[]string{"unknown"}, 2, "unknown command"},
		{[]string{"scan"}, 2, "requires exactly one"},
	} {
		var stdout, stderr bytes.Buffer
		code := run(test.args, &stdout, &stderr)
		combined := stdout.String() + stderr.String()
		if code != test.code || !strings.Contains(combined, test.contains) {
			t.Errorf("run(%v) = %d, %q", test.args, code, combined)
		}
	}
}

func TestScanCommandPlainText(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"a", "b"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("same"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"scan", root}, &stdout, &stderr); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Duplicate group: 2 files") || strings.Contains(stdout.String(), "\x1b[") {
		t.Fatalf("unexpected report:\n%s", stdout.String())
	}
}

func TestScanCommandInvalidRoot(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"scan", filepath.Join(t.TempDir(), "missing")}, &stdout, &stderr); code != 1 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
}
