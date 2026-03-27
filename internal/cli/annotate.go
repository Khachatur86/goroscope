package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/Khachatur86/goroscope/internal/model"
)

// annotateCommand implements `goroscope annotate`.
//
// Usage:
//
//	goroscope annotate capture.gtrace --at=<timeNS|duration> --note="text"
//	goroscope annotate capture.gtrace --list
//	goroscope annotate capture.gtrace --delete=<id>
func annotateCommand(_ context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("annotate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		_, _ = fmt.Fprintf(stderr, "Usage: goroscope annotate [flags] <capture.gtrace>\n\n")
		_, _ = fmt.Fprintf(stderr, "Add, list, or delete annotations inside a .gtrace file.\n")
		_, _ = fmt.Fprintf(stderr, "Annotations appear as named bookmarks in the replay UI.\n\n")
		_, _ = fmt.Fprintf(stderr, "Examples:\n")
		_, _ = fmt.Fprintf(stderr, "  goroscope annotate --at=5s --note=\"latency spike\" run.gtrace\n")
		_, _ = fmt.Fprintf(stderr, "  goroscope annotate --at=1234567890000 --note=\"after deploy v2.3\" run.gtrace\n")
		_, _ = fmt.Fprintf(stderr, "  goroscope annotate --list run.gtrace\n")
		_, _ = fmt.Fprintf(stderr, "  goroscope annotate --delete=ann_1234567890000 run.gtrace\n\n")
		fs.PrintDefaults()
	}

	listFlag := fs.Bool("list", false, "List all annotations in the capture")
	atFlag := fs.String("at", "", "Timestamp: nanoseconds (int) or Go duration from trace start (e.g. 5s, 200ms)")
	noteFlag := fs.String("note", "", "Annotation text to add")
	deleteFlag := fs.String("delete", "", "ID of annotation to delete")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	if fs.NArg() == 0 {
		fs.Usage()
		return fmt.Errorf("missing capture file argument")
	}

	path := fs.Arg(0)

	capture, err := loadCaptureForAnnotation(path)
	if err != nil {
		return fmt.Errorf("load capture %q: %w", path, err)
	}

	switch {
	case *listFlag:
		return listAnnotations(capture, stdout)

	case *deleteFlag != "":
		return deleteAnnotation(capture, path, *deleteFlag, stdout)

	case *noteFlag != "" && *atFlag != "":
		return addAnnotation(capture, path, *atFlag, *noteFlag, stdout)

	default:
		fs.Usage()
		return fmt.Errorf("specify --list, --delete, or both --at and --note")
	}
}

func listAnnotations(capture model.Capture, stdout io.Writer) error {
	if len(capture.Annotations) == 0 {
		_, _ = fmt.Fprintln(stdout, "No annotations.")
		return nil
	}
	_, _ = fmt.Fprintf(stdout, "%-28s  %20s  %s\n", "ID", "TimeNS", "Note")
	_, _ = fmt.Fprintf(stdout, "%s\n", strings.Repeat("-", 70))
	for _, a := range capture.Annotations {
		_, _ = fmt.Fprintf(stdout, "%-28s  %20d  %s\n", a.ID, a.TimeNS, a.Note)
	}
	return nil
}

// loadCaptureForAnnotation reads and unmarshals a .gtrace JSON file without
// any event-content validation (annotate operates on metadata only).
func loadCaptureForAnnotation(path string) (model.Capture, error) {
	//nolint:gosec // path is a CLI argument
	data, err := os.ReadFile(path)
	if err != nil {
		return model.Capture{}, fmt.Errorf("read file: %w", err)
	}
	var cap model.Capture
	if err := json.Unmarshal(data, &cap); err != nil {
		return model.Capture{}, fmt.Errorf("parse JSON: %w", err)
	}
	return cap, nil
}

// saveCaptureForAnnotation writes a capture back as JSON without changing
// indentation style (matches SaveCaptureFile from tracebridge).
func saveCaptureForAnnotation(path string, cap model.Capture) error {
	data, err := json.MarshalIndent(cap, "", "  ")
	if err != nil {
		return fmt.Errorf("encode capture: %w", err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("write capture file %q: %w", path, err)
	}
	return nil
}

func addAnnotation(capture model.Capture, path, atStr, note string, stdout io.Writer) error {
	timeNS, err := parseAnnotationAt(atStr)
	if err != nil {
		return fmt.Errorf("--at: %w", err)
	}

	id := fmt.Sprintf("ann_%d", timeNS)
	// Ensure unique ID by appending a suffix when collision exists.
	for _, a := range capture.Annotations {
		if a.ID == id {
			id = fmt.Sprintf("ann_%d_%d", timeNS, time.Now().UnixNano()%1_000_000)
			break
		}
	}

	capture.Annotations = append(capture.Annotations, model.Annotation{
		ID:     id,
		TimeNS: timeNS,
		Note:   note,
	})

	if err := saveCaptureForAnnotation(path, capture); err != nil {
		return err
	}

	_, _ = fmt.Fprintf(stdout, "Annotation added: id=%s timeNS=%d note=%q\n", id, timeNS, note)
	return nil
}

func deleteAnnotation(capture model.Capture, path, id string, stdout io.Writer) error {
	original := len(capture.Annotations)
	filtered := capture.Annotations[:0]
	for _, a := range capture.Annotations {
		if a.ID != id {
			filtered = append(filtered, a)
		}
	}
	if len(filtered) == original {
		return fmt.Errorf("annotation %q not found", id)
	}
	capture.Annotations = filtered

	if err := saveCaptureForAnnotation(path, capture); err != nil {
		return err
	}

	_, _ = fmt.Fprintf(stdout, "Annotation %q deleted.\n", id)
	return nil
}

// annotationsToBookmarkParam encodes annotations as the frontend ?bm= URL
// parameter so the UI loads them as named timeline bookmarks on open.
// Format matches bookmarks.ts: "name1:timeNS1,name2:timeNS2,...".
// Returns an empty string when there are no annotations.
func annotationsToBookmarkParam(annotations []model.Annotation) string {
	if len(annotations) == 0 {
		return ""
	}
	parts := make([]string, len(annotations))
	for i, a := range annotations {
		parts[i] = fmt.Sprintf("%s:%d", strings.ReplaceAll(a.Note, ",", " "), a.TimeNS)
	}
	return "?bm=" + strings.Join(parts, ",")
}

// parseAnnotationAt parses --at=<value> into nanoseconds.
// Accepts:
//   - Plain int64 (nanosecond timestamp): "1234567890000"
//   - Go duration string (offset from zero): "5s", "200ms", "1m30s"
func parseAnnotationAt(s string) (int64, error) {
	// Try duration first (contains non-digit characters).
	if strings.ContainsAny(s, "smhµun") {
		d, err := time.ParseDuration(s)
		if err != nil {
			return 0, fmt.Errorf("invalid duration %q: %w", s, err)
		}
		return d.Nanoseconds(), nil
	}
	// Fall back to raw int64.
	var ns int64
	if _, err := fmt.Sscanf(s, "%d", &ns); err != nil {
		return 0, fmt.Errorf("expected nanosecond int or duration (e.g. 5s), got %q", s)
	}
	if ns < 0 {
		return 0, fmt.Errorf("timestamp must be non-negative, got %d", ns)
	}
	return ns, nil
}
