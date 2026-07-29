package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"

	tea "charm.land/bubbletea/v2"
	"github.com/xlukacs/Deduper/internal/app"
	"github.com/xlukacs/Deduper/internal/report"
	"github.com/xlukacs/Deduper/internal/scan"
)

// version can be replaced by release builds with -X main.version.
var version = "0.1.0"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		if _, err := tea.NewProgram(app.New()).Run(); err != nil {
			fmt.Fprintln(stderr, "deduper:", err)
			return 1
		}
		return 0
	}

	switch args[0] {
	case "help", "--help", "-h":
		if len(args) != 1 {
			return usageError(stderr, "help takes no arguments")
		}
		printUsage(stdout)
		return 0
	case "version", "--version", "-v":
		if len(args) != 1 {
			return usageError(stderr, "version takes no arguments")
		}
		fmt.Fprintf(stdout, "deduper %s\n", version)
		return 0
	case "scan":
		if len(args) != 2 {
			return usageError(stderr, "scan requires exactly one folder path")
		}
		return runScan(args[1], stdout, stderr)
	default:
		return usageError(stderr, fmt.Sprintf("unknown command %q", args[0]))
	}
}

func runScan(root string, stdout, stderr io.Writer) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	result, err := scan.Scan(ctx, root, stderrObserver{writer: stderr})
	if err != nil {
		if errors.Is(err, context.Canceled) {
			fmt.Fprintln(stderr, "deduper: interrupted")
			return 130
		}
		fmt.Fprintln(stderr, "deduper:", err)
		return 1
	}
	if err := report.Text(stdout, result); err != nil {
		fmt.Fprintln(stderr, "deduper: write report:", err)
		return 1
	}
	return 0
}

type stderrObserver struct{ writer io.Writer }

func (stderrObserver) OnProgress(scan.Progress) {}
func (o stderrObserver) OnWarning(warning scan.Warning) {
	fmt.Fprintf(o.writer, "warning: %s: %v\n", warning.Path, warning.Err)
}

func usageError(stderr io.Writer, message string) int {
	fmt.Fprintln(stderr, "deduper:", message)
	printUsage(stderr)
	return 2
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  deduper             open the interactive application")
	fmt.Fprintln(w, "  deduper scan PATH   scan and print a text report")
	fmt.Fprintln(w, "  deduper help")
	fmt.Fprintln(w, "  deduper version")
}
