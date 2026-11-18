// Package authlog stores authentication outcomes separately from the database.
package authlog

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

const (
	Unknown = iota
	Internal
	LDAP
	OAuth
)

const (
	Success = iota + 1
	Failure
	Logout
)

// Event contains only information safe to persist for an authentication outcome.
type Event struct {
	ID                    string `json:"id"`
	Timestamp             int64  `json:"timestamp"`
	Username              string `json:"username"`
	Context               int    `json:"context"`
	Type                  int    `json:"type"`
	Origin                string `json:"origin"`
	StatusCode            int    `json:"statusCode"`
	UserID                int    `json:"userId,omitempty"`
	RateLimited           bool   `json:"rateLimited,omitempty"`
	SuppressedRateLimited uint64 `json:"suppressedRateLimited,omitempty"`
}

type SourceStatus struct {
	Available bool   `json:"available"`
	Partial   bool   `json:"partial"`
	Message   string `json:"message"`
}

// Store serializes appends and snapshots in this server process. A failed
// append is remembered for the lifetime of the process so readers are warned
// that the historical record may be incomplete even after storage recovers.
type Store struct {
	mu           sync.Mutex
	dir          string
	partial      bool
	readers      map[string]int
	pendingPrune map[string]bool
}

func NewStore(datastorePath string) *Store {
	s := &Store{dir: filepath.Join(datastorePath, "logs", "authentication")}
	s.mu.Lock()
	if err := os.MkdirAll(s.dir, 0700); err != nil {
		s.recordFailure()
	} else if err := s.pruneLocked(time.Now()); err != nil {
		s.recordFailure()
	}
	s.mu.Unlock()
	return s
}

// Start prunes at each UTC day boundary until the server shuts down.
func (s *Store) Start(ctx context.Context) {
	go func() {
		for {
			now := time.Now().UTC()
			next := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)
			timer := time.NewTimer(time.Until(next))
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
				s.mu.Lock()
				if err := s.pruneLocked(time.Now()); err != nil {
					s.recordFailure()
				}
				s.mu.Unlock()
			}
		}
	}()
}

func (s *Store) recordFailure() {
	s.partial = true
	// Deliberately omit OS errors and paths from operational logs.
	log.Warn().Msg("authentication log storage operation failed; records may be incomplete")
}

// Append writes one JSON line. A newline before a torn tail prevents the next
// valid event from becoming part of the damaged line.
func (s *Store) Append(event Event) error {
	if s == nil {
		return errors.New("authentication log store is unavailable")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if event.Timestamp == 0 {
		event.Timestamp = time.Now().UTC().Unix()
	}
	if event.ID == "" {
		var id [16]byte
		if _, err := rand.Read(id[:]); err != nil {
			s.recordFailure()
			return err
		}
		event.ID = hex.EncodeToString(id[:])
	}
	data, err := json.Marshal(event)
	if err != nil {
		s.recordFailure()
		return err
	}
	if err := os.MkdirAll(s.dir, 0700); err != nil {
		s.recordFailure()
		return err
	}
	name := time.Unix(event.Timestamp, 0).UTC().Format("2006-01-02") + ".jsonl"
	f, err := os.OpenFile(filepath.Join(s.dir, name), os.O_CREATE|os.O_RDWR|os.O_APPEND, 0600)
	if err != nil {
		s.recordFailure()
		return err
	}
	info, err := f.Stat()
	if err == nil && info.Size() > 0 {
		var last [1]byte
		_, err = f.ReadAt(last[:], info.Size()-1)
		if err == nil && last[0] != '\n' {
			// Invalidate even a syntactically complete but unterminated tail so
			// it cannot be mistaken for a committed event after a restart.
			data = append([]byte{'!', '\n'}, data...)
			s.partial = true
		}
	}
	if err == nil {
		data = append(data, '\n')
		var n int
		n, err = f.Write(data)
		if err == nil && n != len(data) {
			err = io.ErrShortWrite
		}
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		s.recordFailure()
	}
	return err
}

// Read returns a snapshot and reports unreadable or corrupt portions without
// discarding valid records. No filesystem error details are returned to clients.
func (s *Store) Read(ctx context.Context) ([]Event, SourceStatus, error) {
	events := make([]Event, 0)
	if err := ctx.Err(); err != nil {
		return nil, SourceStatus{}, err
	}
	if s == nil {
		return events, SourceStatus{Message: "Authentication log storage is unavailable."}, nil
	}
	files, status, err := s.snapshot(ctx)
	if err != nil {
		return nil, status, err
	}
	defer s.releaseSnapshot(files)
	for i := range files {
		if err := ctx.Err(); err != nil {
			return nil, status, err
		}
		file := &files[i]
		limited := &io.LimitedReader{R: file.handle, N: file.size}
		reader := bufio.NewReader(limited)
		for {
			if err := ctx.Err(); err != nil {
				return nil, status, err
			}
			line, oversized, readErr := readLine(ctx, reader)
			if ctx.Err() != nil {
				return nil, status, ctx.Err()
			}
			if oversized {
				status.Partial = true
			}
			if len(line) > 0 {
				var event Event
				if oversized || line[len(line)-1] != '\n' || json.Unmarshal(line, &event) != nil || !validEvent(event) {
					status.Partial = true
				} else {
					events = append(events, event)
					if event.RateLimited {
						status.Message = "Rate-limited requests are sampled globally; suppressed requests are counted on the next sample."
					}
				}
			}
			if readErr != nil {
				if !errors.Is(readErr, io.EOF) || limited.N > 0 {
					status.Partial = true
				}
				break
			}
		}
		if err := file.handle.Close(); err != nil {
			status.Partial = true
		}
		file.handle = nil
	}
	if status.Available && status.Partial {
		status.Message = "Some authentication records may be missing or unreadable."
	}
	return events, status, nil
}

type snapshotFile struct {
	name   string
	handle *os.File
	size   int64
}

// snapshot opens the files and records their lengths while appends and pruning
// are excluded. Parsing happens after the lock has been released.
func (s *Store) snapshot(ctx context.Context) ([]snapshotFile, SourceStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	status := SourceStatus{Available: true, Partial: s.partial}
	if err := ctx.Err(); err != nil {
		return nil, status, err
	}
	entries, err := os.ReadDir(s.dir)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, status, ctxErr
	}
	if err != nil {
		if errors.Is(err, os.ErrNotExist) && !s.partial {
			return nil, status, nil
		}
		return nil, SourceStatus{Available: false, Partial: s.partial, Message: "Authentication log storage is unavailable."}, nil
	}
	files := make([]snapshotFile, 0, len(entries))
	cutoff := retentionCutoff(time.Now())
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			for _, file := range files {
				_ = file.handle.Close()
			}
			return nil, status, err
		}
		if !isLogFile(entry.Name()) {
			continue
		}
		date, _ := time.Parse("2006-01-02", strings.TrimSuffix(entry.Name(), ".jsonl"))
		if s.pendingPrune[entry.Name()] || date.Before(cutoff) {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			status.Partial = true
			continue
		}
		file, err := os.Open(filepath.Join(s.dir, entry.Name()))
		if err != nil {
			status.Partial = true
			continue
		}
		info, err = file.Stat()
		if err != nil || !info.Mode().IsRegular() {
			_ = file.Close()
			status.Partial = true
			continue
		}
		files = append(files, snapshotFile{name: entry.Name(), handle: file, size: info.Size()})
	}
	if err := ctx.Err(); err != nil {
		for _, file := range files {
			_ = file.handle.Close()
		}
		return nil, status, err
	}
	if s.readers == nil {
		s.readers = make(map[string]int)
	}
	for _, file := range files {
		s.readers[file.name]++
	}
	return files, status, nil
}

// Defer removal of an expired file until its final snapshot handle has closed.
// Windows in particular may not allow an open file to be removed.
func (s *Store) releaseSnapshot(files []snapshotFile) {
	for _, file := range files {
		if file.handle != nil {
			_ = file.handle.Close()
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, file := range files {
		s.readers[file.name]--
		if s.readers[file.name] != 0 {
			continue
		}
		delete(s.readers, file.name)
		if s.pendingPrune[file.name] {
			delete(s.pendingPrune, file.name)
			if err := os.Remove(filepath.Join(s.dir, file.name)); err != nil && !errors.Is(err, os.ErrNotExist) {
				s.recordFailure()
			}
		}
	}
}

// readLine bounds memory consumed by a corrupt line while still advancing to
// the next line so subsequent valid records remain readable.
func readLine(ctx context.Context, reader *bufio.Reader) ([]byte, bool, error) {
	const maxLine = 1 << 20
	var line bytes.Buffer
	oversized := false
	for {
		if err := ctx.Err(); err != nil {
			return nil, oversized, err
		}
		fragment, err := reader.ReadSlice('\n')
		if !oversized && line.Len()+len(fragment) <= maxLine {
			_, _ = line.Write(fragment)
		} else {
			oversized = true
		}
		if !errors.Is(err, bufio.ErrBufferFull) {
			return line.Bytes(), oversized, err
		}
	}
}

func validEvent(e Event) bool {
	return e.ID != "" && e.Timestamp > 0 && e.Context >= Unknown && e.Context <= OAuth && e.Type >= Success && e.Type <= Logout && e.StatusCode >= 100 && e.StatusCode <= 599
}

func isLogFile(name string) bool {
	if !strings.HasSuffix(name, ".jsonl") {
		return false
	}
	date := strings.TrimSuffix(name, ".jsonl")
	parsed, err := time.Parse("2006-01-02", date)
	return err == nil && parsed.Format("2006-01-02") == date
}

func retentionCutoff(now time.Time) time.Time {
	now = now.UTC()
	return time.Date(now.Year(), now.Month(), now.Day()-29, 0, 0, 0, 0, time.UTC)
}

func (s *Store) pruneLocked(now time.Time) error {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return err
	}
	cutoff := retentionCutoff(now)
	var result error
	for _, entry := range entries {
		if entry.IsDir() || !isLogFile(entry.Name()) {
			continue
		}
		date, _ := time.Parse("2006-01-02", strings.TrimSuffix(entry.Name(), ".jsonl"))
		if date.Before(cutoff) {
			if s.readers[entry.Name()] > 0 {
				if s.pendingPrune == nil {
					s.pendingPrune = make(map[string]bool)
				}
				s.pendingPrune[entry.Name()] = true
				continue
			}
			if err := os.Remove(filepath.Join(s.dir, entry.Name())); err != nil {
				result = err
			}
		}
	}
	return result
}
