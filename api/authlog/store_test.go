package authlog

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestStoreAppendReadSurvivesRestart(t *testing.T) {
	t.Parallel()

	datastorePath := t.TempDir()
	first := NewStore(datastorePath)
	want := testEvent("event-1", time.Now().UTC().Unix())
	require.NoError(t, first.Append(want))

	// A new store instance represents a process restart and must read the same
	// file without relying on in-memory state from the writer.
	second := NewStore(datastorePath)
	got, status, err := second.Read(context.Background())
	require.NoError(t, err)
	require.Equal(t, SourceStatus{Available: true}, status)
	require.Equal(t, []Event{want}, got)

	info, err := os.Stat(filepath.Join(datastorePath, "logs", "authentication"))
	require.NoError(t, err)
	require.True(t, info.IsDir())
}

func TestStoreAppendWritesOnlyEventFields(t *testing.T) {
	t.Parallel()

	datastorePath := t.TempDir()
	store := NewStore(datastorePath)
	event := testEvent("event-safe-fields", time.Now().UTC().Unix())
	event.UserID = 42
	require.NoError(t, store.Append(event))

	contents, err := os.ReadFile(filepath.Join(datastorePath, "logs", "authentication", time.Unix(event.Timestamp, 0).UTC().Format("2006-01-02")+".jsonl"))
	require.NoError(t, err)
	require.True(t, strings.HasSuffix(string(contents), "\n"))

	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(bytesTrimNewline(contents), &fields))
	require.Equal(t, []string{"context", "id", "origin", "statusCode", "timestamp", "type", "userId", "username"}, sortedKeys(fields))
	require.NotContains(t, string(contents), "password")
	require.NotContains(t, string(contents), "token")
	require.NotContains(t, string(contents), "error")
}

func TestStoreConcurrentAppend(t *testing.T) {
	t.Parallel()

	const count = 80
	store := NewStore(t.TempDir())
	var wg sync.WaitGroup
	errs := make(chan error, count)
	for i := range count {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs <- store.Append(testEvent(fmt.Sprintf("event-%d", i), time.Now().UTC().Unix()))
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	events, status, err := store.Read(context.Background())
	require.NoError(t, err)
	require.Equal(t, SourceStatus{Available: true}, status)
	require.Len(t, events, count)
	seen := make(map[string]struct{}, count)
	for _, event := range events {
		_, duplicate := seen[event.ID]
		require.False(t, duplicate, "duplicate event id %q", event.ID)
		seen[event.ID] = struct{}{}
	}
}

func TestNewStorePrunesFilesOutsideThirtyDayRetention(t *testing.T) {
	t.Parallel()

	datastorePath := t.TempDir()
	logDir := filepath.Join(datastorePath, "logs", "authentication")
	require.NoError(t, os.MkdirAll(logDir, 0700))
	today := time.Now().UTC().Truncate(24 * time.Hour)
	expired := today.AddDate(0, 0, -30).Format("2006-01-02") + ".jsonl"
	oldestRetained := today.AddDate(0, 0, -29).Format("2006-01-02") + ".jsonl"
	for _, name := range []string{expired, oldestRetained, "not-a-date.jsonl", "notes.txt"} {
		require.NoError(t, os.WriteFile(filepath.Join(logDir, name), []byte("fixture\n"), 0600))
	}

	_ = NewStore(datastorePath)

	_, err := os.Stat(filepath.Join(logDir, expired))
	require.ErrorIs(t, err, os.ErrNotExist)
	for _, name := range []string{oldestRetained, "not-a-date.jsonl", "notes.txt"} {
		_, err := os.Stat(filepath.Join(logDir, name))
		require.NoError(t, err, "unexpected deletion of %s", name)
	}
}

func TestStoreReadMarksMalformedAndTornRowsPartial(t *testing.T) {
	t.Parallel()

	datastorePath := t.TempDir()
	store := NewStore(datastorePath)
	event := testEvent("valid-row", time.Now().UTC().Unix())
	valid, err := json.Marshal(event)
	require.NoError(t, err)
	contents := append(append(append([]byte(nil), valid...), '\n'), []byte("{malformed}\n")...)
	contents = append(contents, valid...) // valid JSON without a terminating newline is still a torn row.
	name := time.Unix(event.Timestamp, 0).UTC().Format("2006-01-02") + ".jsonl"
	require.NoError(t, os.WriteFile(filepath.Join(datastorePath, "logs", "authentication", name), contents, 0600))

	events, status, err := store.Read(context.Background())
	require.NoError(t, err)
	require.Equal(t, []Event{event}, events, "a torn tail must not be exposed as a complete event")
	require.True(t, status.Available)
	require.True(t, status.Partial)
	require.NotEmpty(t, status.Message)
}

func TestStoreAppendAfterTornTailSeparatesNextRecord(t *testing.T) {
	t.Parallel()

	datastorePath := t.TempDir()
	store := NewStore(datastorePath)
	timestamp := time.Now().UTC().Unix()
	name := time.Unix(timestamp, 0).UTC().Format("2006-01-02") + ".jsonl"
	path := filepath.Join(datastorePath, "logs", "authentication", name)
	require.NoError(t, os.WriteFile(path, []byte("{torn"), 0600))

	newEvent := testEvent("after-torn-tail", timestamp)
	require.NoError(t, store.Append(newEvent))
	contents, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(contents), "{torn!\n")
	require.True(t, strings.HasSuffix(string(contents), "\n"))

	events, status, err := store.Read(context.Background())
	require.NoError(t, err)
	require.Equal(t, []Event{newEvent}, events)
	require.True(t, status.Partial)
}

func TestStoreReadHonorsCanceledContext(t *testing.T) {
	t.Parallel()

	store := NewStore(t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _, err := store.Read(ctx)
	require.ErrorIs(t, err, context.Canceled)
}

func TestStoreSnapshotDoesNotBlockAppendsOrIncludeLaterWrites(t *testing.T) {
	t.Parallel()

	store := NewStore(t.TempDir())
	timestamp := time.Now().UTC().Unix()
	first := testEvent("before-snapshot", timestamp)
	second := testEvent("after-snapshot", timestamp)
	require.NoError(t, store.Append(first))

	files, status, err := store.snapshot(context.Background())
	require.NoError(t, err)
	require.Equal(t, SourceStatus{Available: true}, status)
	require.Len(t, files, 1)
	defer store.releaseSnapshot(files)

	appendResult := make(chan error, 1)
	go func() { appendResult <- store.Append(second) }()
	select {
	case err := <-appendResult:
		require.NoError(t, err, "append should not wait for snapshot scanning")
	case <-time.After(time.Second):
		t.Fatal("append blocked while a snapshot was open")
	}

	limited := &io.LimitedReader{R: files[0].handle, N: files[0].size}
	line, oversized, err := readLine(context.Background(), bufio.NewReader(limited))
	require.NoError(t, err)
	require.False(t, oversized)
	var got Event
	require.NoError(t, json.Unmarshal(line, &got))
	require.Equal(t, first, got)

	events, status, err := store.Read(context.Background())
	require.NoError(t, err)
	require.Equal(t, SourceStatus{Available: true}, status)
	require.ElementsMatch(t, []Event{first, second}, events)
}

func TestStorePendingPruneExcludesNewSnapshotsAndWaitsForExistingReader(t *testing.T) {
	t.Parallel()

	datastorePath := t.TempDir()
	store := NewStore(datastorePath)
	event := testEvent("retained-snapshot", time.Now().UTC().Unix())
	require.NoError(t, store.Append(event))
	name := time.Unix(event.Timestamp, 0).UTC().Format("2006-01-02") + ".jsonl"
	path := filepath.Join(datastorePath, "logs", "authentication", name)

	existing, _, err := store.snapshot(context.Background())
	require.NoError(t, err)
	require.Len(t, existing, 1)

	store.mu.Lock()
	store.pendingPrune = map[string]bool{name: true}
	store.mu.Unlock()

	newSnapshot, status, err := store.snapshot(context.Background())
	require.NoError(t, err)
	require.Empty(t, newSnapshot, "new snapshots must skip files pending retention deletion")
	require.Equal(t, SourceStatus{Available: true}, status)
	store.releaseSnapshot(newSnapshot)

	_, err = os.Stat(path)
	require.NoError(t, err, "an existing reader must keep the file open until it releases the snapshot")
	store.releaseSnapshot(existing)
	_, err = os.Stat(path)
	require.ErrorIs(t, err, os.ErrNotExist, "the final reader release must finish the pending prune")
}

func testEvent(id string, timestamp int64) Event {
	return Event{
		ID:         id,
		Timestamp:  timestamp,
		Username:   "alice",
		Context:    Internal,
		Type:       Success,
		Origin:     "10.0.0.7",
		StatusCode: 200,
	}
}

func bytesTrimNewline(value []byte) []byte {
	return []byte(strings.TrimSuffix(string(value), "\n"))
}

func sortedKeys(fields map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
