package cache

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/cronpulser/cronpulser-runner/internal/protocol"
)

const maxEntries = 1000

// interruptedReportWindow limits which executions interrupted by a restart are
// reported to the server. Older entries predate this release or were already
// resolved, and reporting them would raise stale alerts.
const interruptedReportWindow = 24 * time.Hour

type Entry struct {
	State     string           `json:"state"`
	Message   protocol.Message `json:"message"`
	UpdatedAt time.Time        `json:"updatedAt"`
	// Pending marks a final result that has not yet been written to a server
	// connection. Pending results are sent again after the runner reconnects.
	Pending bool `json:"pending,omitempty"`
}

type Cache struct {
	mu      sync.Mutex
	path    string
	entries map[string]Entry
}

func isFinal(state string) bool {
	return state == protocol.TypeFinished || state == protocol.TypeFailed
}

func Load(path string) (*Cache, error) {
	cache := &Cache{path: path, entries: make(map[string]Entry)}
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("read execution cache: %w", err)
	}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &cache.entries); err != nil {
			return nil, fmt.Errorf("parse execution cache: %w", err)
		}
	}
	changed := false
	for id, entry := range cache.entries {
		if entry.State == protocol.TypeAccepted || entry.State == protocol.TypeStarted {
			now := time.Now().UTC()
			exit := -1
			errText := "runner restarted while execution was in progress"
			entry.Pending = now.Sub(entry.UpdatedAt) < interruptedReportWindow
			entry.State = protocol.TypeFailed
			entry.Message = protocol.Message{Type: protocol.TypeFailed, ExecutionID: id, Timestamp: now, FinishedAt: &now, ExitCode: &exit, Error: &errText}
			entry.UpdatedAt = now
			cache.entries[id] = entry
			changed = true
		}
	}
	if changed {
		return cache, cache.persistLocked()
	}
	return cache, nil
}

func (c *Cache) Get(id string) (Entry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[id]
	return entry, ok
}

// Put records the latest state of an execution. Final states start as
// pending until MarkDelivered confirms they were written to the server.
func (c *Cache) Put(id, state string, message protocol.Message) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[id] = Entry{State: state, Message: message, UpdatedAt: time.Now().UTC(), Pending: isFinal(state)}
	if len(c.entries) > maxEntries {
		type pair struct {
			id      string
			updated time.Time
		}
		items := make([]pair, 0, len(c.entries))
		for entryID, entry := range c.entries {
			items = append(items, pair{entryID, entry.UpdatedAt})
		}
		sort.Slice(items, func(i, j int) bool { return items[i].updated.Before(items[j].updated) })
		for _, item := range items[:len(items)-maxEntries] {
			delete(c.entries, item.id)
		}
	}
	return c.persistLocked()
}

// IsPending reports whether the final result of an execution still has to be
// sent to the server.
func (c *Cache) IsPending(id string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.entries[id].Pending
}

// Pending returns the undelivered final results, oldest first.
func (c *Cache) Pending() []protocol.Message {
	c.mu.Lock()
	defer c.mu.Unlock()
	entries := make([]Entry, 0)
	for _, entry := range c.entries {
		if entry.Pending {
			entries = append(entries, entry)
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].UpdatedAt.Before(entries[j].UpdatedAt) })
	messages := make([]protocol.Message, len(entries))
	for i, entry := range entries {
		messages[i] = entry.Message
	}
	return messages
}

// MarkDelivered records that a final result was written to a server
// connection, so it is not sent again after a reconnect.
func (c *Cache) MarkDelivered(id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[id]
	if !ok || !entry.Pending {
		return nil
	}
	entry.Pending = false
	c.entries[id] = entry
	return c.persistLocked()
}

func (c *Cache) persistLocked() error {
	if err := os.MkdirAll(filepath.Dir(c.path), 0750); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c.entries, "", "  ")
	if err != nil {
		return err
	}
	temporary := c.path + ".tmp"
	if err := os.WriteFile(temporary, data, 0600); err != nil {
		return err
	}
	if err := os.Rename(temporary, c.path); err != nil {
		return err
	}
	return nil
}
