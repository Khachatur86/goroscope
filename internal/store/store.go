// Package store manages the persistent capture history under ~/.goroscope/captures.
package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Khachatur86/goroscope/internal/model"
	"github.com/Khachatur86/goroscope/internal/tracebridge"
)

const indexFile = "index.json"

// Entry describes one persisted capture in the history index.
type Entry struct {
	ID             string    `json:"id"`
	Filename       string    `json:"filename"`        // relative to store dir
	Target         string    `json:"target"`
	CreatedAt      time.Time `json:"created_at"`
	DurationNS     int64     `json:"duration_ns"`
	GoroutineCount int       `json:"goroutine_count"`
}

// diskIndex is the on-disk format of the history index.
type diskIndex struct {
	Entries []Entry `json:"entries"`
}

// Store manages persisted captures under a single directory.
type Store struct {
	dir string
}

// DefaultDir returns the default capture store directory (~/.goroscope/captures).
func DefaultDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".goroscope", "captures"), nil
}

// New creates a Store rooted at dir, creating the directory if it does not exist.
func New(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create capture store %q: %w", dir, err)
	}
	return &Store{dir: dir}, nil
}

// SaveInput holds parameters for Store.Save.
type SaveInput struct {
	Capture   model.Capture
	Target    string
	CreatedAt time.Time
}

// Save persists capture to the store and appends an entry to the index.
// It returns the absolute path of the saved .gtrace file.
func (s *Store) Save(in SaveInput) (string, error) {
	id := in.CreatedAt.UTC().Format("20060102-150405") + fmt.Sprintf("-%d", in.CreatedAt.UnixNano()%1e9)
	filename := id + ".gtrace"
	path := filepath.Join(s.dir, filename)

	if err := tracebridge.SaveCaptureFile(path, in.Capture); err != nil {
		return "", fmt.Errorf("write capture: %w", err)
	}

	entry := Entry{
		ID:             id,
		Filename:       filename,
		Target:         in.Target,
		CreatedAt:      in.CreatedAt.UTC(),
		DurationNS:     captureDurationNS(in.Capture),
		GoroutineCount: captureGoroutineCount(in.Capture),
	}
	if err := s.appendEntry(entry); err != nil {
		// The file was already written; best effort to update index.
		return path, fmt.Errorf("update index: %w", err)
	}

	return path, nil
}

// List returns all stored entries, oldest first.
func (s *Store) List() ([]Entry, error) {
	idx, err := s.loadIndex()
	if err != nil {
		return nil, err
	}
	return idx.Entries, nil
}

// FilePath returns the absolute path to the capture file for the given entry.
func (s *Store) FilePath(e Entry) string {
	return filepath.Join(s.dir, e.Filename)
}

func (s *Store) appendEntry(e Entry) error {
	idx, err := s.loadIndex()
	if err != nil {
		return err
	}
	idx.Entries = append(idx.Entries, e)
	return s.saveIndex(idx)
}

func (s *Store) loadIndex() (diskIndex, error) {
	path := filepath.Join(s.dir, indexFile)
	data, err := os.ReadFile(path) //nolint:gosec
	if os.IsNotExist(err) {
		return diskIndex{}, nil
	}
	if err != nil {
		return diskIndex{}, fmt.Errorf("read capture index: %w", err)
	}
	var idx diskIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		return diskIndex{}, fmt.Errorf("decode capture index: %w", err)
	}
	return idx, nil
}

func (s *Store) saveIndex(idx diskIndex) error {
	path := filepath.Join(s.dir, indexFile)
	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return fmt.Errorf("encode capture index: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write capture index: %w", err)
	}
	return nil
}

// captureDurationNS returns the span between the earliest and latest event
// timestamp in the capture, in nanoseconds.
func captureDurationNS(c model.Capture) int64 {
	var minNS, maxNS int64
	for i, ev := range c.Events {
		ns := ev.Timestamp.UnixNano()
		if i == 0 || ns < minNS {
			minNS = ns
		}
		if ns > maxNS {
			maxNS = ns
		}
	}
	if maxNS <= minNS {
		return 0
	}
	return maxNS - minNS
}

// captureGoroutineCount returns the number of unique goroutine IDs in capture.
func captureGoroutineCount(c model.Capture) int {
	ids := make(map[int64]struct{}, len(c.Events))
	for _, ev := range c.Events {
		ids[ev.GoroutineID] = struct{}{}
	}
	return len(ids)
}
