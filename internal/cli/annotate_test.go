package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Khachatur86/goroscope/internal/model"
)

func writeTempCapture(t *testing.T, capture model.Capture) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.gtrace")
	data, err := json.MarshalIndent(capture, "", "  ")
	if err != nil {
		t.Fatalf("marshal capture: %v", err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatalf("write capture: %v", err)
	}
	return path
}

func TestAnnotateCommand_MissingFile(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	err := annotateCommand(context.Background(), []string{"--list"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected error for missing file argument")
	}
}

func TestAnnotateCommand_ListEmpty(t *testing.T) {
	t.Parallel()
	path := writeTempCapture(t, model.Capture{Name: "test"})

	var stdout, stderr bytes.Buffer
	err := annotateCommand(context.Background(), []string{"--list", path}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout.String(), "No annotations") {
		t.Errorf("expected 'No annotations', got %q", stdout.String())
	}
}

func TestAnnotateCommand_AddAndList(t *testing.T) {
	t.Parallel()
	path := writeTempCapture(t, model.Capture{Name: "test"})

	var stdout, stderr bytes.Buffer
	// Add annotation with duration at.
	err := annotateCommand(context.Background(), []string{"--at=5s", "--note=latency spike", path}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("add annotation: %v", err)
	}
	if !strings.Contains(stdout.String(), "latency spike") {
		t.Errorf("expected note in output, got %q", stdout.String())
	}

	// Verify it was saved by listing.
	stdout.Reset()
	err = annotateCommand(context.Background(), []string{"--list", path}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("list annotations: %v", err)
	}
	if !strings.Contains(stdout.String(), "latency spike") {
		t.Errorf("expected annotation in list, got %q", stdout.String())
	}
}

func TestAnnotateCommand_AddWithRawNS(t *testing.T) {
	t.Parallel()
	path := writeTempCapture(t, model.Capture{Name: "test"})

	var stdout, stderr bytes.Buffer
	err := annotateCommand(context.Background(), []string{"--at=1000000000", "--note=one second mark", path}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify timeNS stored correctly.
	cap2, err := loadCaptureForAnnotation(path)
	if err != nil {
		t.Fatalf("reload capture: %v", err)
	}
	if len(cap2.Annotations) != 1 {
		t.Fatalf("expected 1 annotation, got %d", len(cap2.Annotations))
	}
	if cap2.Annotations[0].TimeNS != 1_000_000_000 {
		t.Errorf("expected timeNS=1000000000, got %d", cap2.Annotations[0].TimeNS)
	}
}

func TestAnnotateCommand_Delete(t *testing.T) {
	t.Parallel()
	cap := model.Capture{
		Name: "test",
		Annotations: []model.Annotation{
			{ID: "ann_100", TimeNS: 100, Note: "keep me"},
			{ID: "ann_200", TimeNS: 200, Note: "delete me"},
		},
	}
	path := writeTempCapture(t, cap)

	var stdout, stderr bytes.Buffer
	err := annotateCommand(context.Background(), []string{"--delete=ann_200", path}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("delete annotation: %v", err)
	}

	cap2, err := loadCaptureForAnnotation(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if len(cap2.Annotations) != 1 {
		t.Fatalf("expected 1 annotation after delete, got %d", len(cap2.Annotations))
	}
	if cap2.Annotations[0].ID != "ann_100" {
		t.Errorf("wrong annotation remains: %q", cap2.Annotations[0].ID)
	}
}

func TestAnnotateCommand_DeleteNotFound(t *testing.T) {
	t.Parallel()
	path := writeTempCapture(t, model.Capture{Name: "test"})

	var stdout, stderr bytes.Buffer
	err := annotateCommand(context.Background(), []string{"--delete=nonexistent", path}, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected error for nonexistent ID")
	}
}

func TestParseAnnotationAt(t *testing.T) {
	t.Parallel()
	cases := []struct {
		input   string
		wantNS  int64
		wantErr bool
	}{
		{"5s", 5_000_000_000, false},
		{"200ms", 200_000_000, false},
		{"1m30s", 90_000_000_000, false},
		{"1000000", 1_000_000, false},
		{"0", 0, false},
		{"-1", 0, true},
		{"notaduration", 0, true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			got, err := parseAnnotationAt(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Errorf("expected error for %q, got nil", tc.input)
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error for %q: %v", tc.input, err)
				return
			}
			if got != tc.wantNS {
				t.Errorf("parseAnnotationAt(%q) = %d, want %d", tc.input, got, tc.wantNS)
			}
		})
	}
}

func TestAnnotationsToBookmarkParam(t *testing.T) {
	t.Parallel()

	empty := annotationsToBookmarkParam(nil)
	if empty != "" {
		t.Errorf("empty annotations should return empty string, got %q", empty)
	}

	anns := []model.Annotation{
		{ID: "a1", TimeNS: 5_000_000_000, Note: "start"},
		{ID: "a2", TimeNS: 10_000_000_000, Note: "end"},
	}
	param := annotationsToBookmarkParam(anns)
	if !strings.HasPrefix(param, "?bm=") {
		t.Errorf("expected ?bm= prefix, got %q", param)
	}
	if !strings.Contains(param, "start:5000000000") {
		t.Errorf("expected start annotation in param, got %q", param)
	}
	if !strings.Contains(param, "end:10000000000") {
		t.Errorf("expected end annotation in param, got %q", param)
	}
}
