package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

// ModelUsage counts observed upstream completions, not incoming HTTP requests.
// Cache and reasoning tokens are subsets of prompt and completion tokens.
type ModelUsage struct {
	CompletedRequests    int64 `json:"completed_requests"`
	MeasuredRequests     int64 `json:"measured_requests"`
	UnknownUsageRequests int64 `json:"unknown_usage_requests"`
	PromptTokens         int64 `json:"prompt_tokens"`
	CompletionTokens     int64 `json:"completion_tokens"`
	TotalTokens          int64 `json:"total_tokens"`
	CachedTokens         int64 `json:"cached_tokens"`
	ReasoningTokens      int64 `json:"reasoning_tokens"`
}

type PersistenceStatus struct {
	Enabled bool   `json:"enabled"`
	Healthy bool   `json:"healthy"`
	Error   string `json:"error,omitempty"`
}

type GlobalUsageStats struct {
	SchemaVersion int `json:"schema_version"`
	ModelUsage
	TrackingStartedAt time.Time              `json:"tracking_started_at"`
	LastCompletedAt   time.Time              `json:"last_completed_at"`
	Models            map[string]*ModelUsage `json:"models"`
	Persistence       PersistenceStatus      `json:"persistence"`
}

type UsageTracker struct {
	mu           sync.RWMutex
	stats        GlobalUsageStats
	filePath     string
	explicitFile bool
	now          func() time.Time
	blocked      bool
	closed       bool
	lockFile     *os.File
	closeErr     error
}

type UsageTrackerOption func(*UsageTracker)

// WithUsageFile overrides the persistent state file. Empty disables persistence.
func WithUsageFile(path string) UsageTrackerOption {
	return func(t *UsageTracker) { t.filePath = path; t.explicitFile = true }
}

// WithUsageClock injects the clock, primarily for isolated deterministic tests.
func WithUsageClock(now func() time.Time) UsageTrackerOption {
	return func(t *UsageTracker) {
		if now != nil {
			t.now = now
		}
	}
}

func defaultUsageFile() (string, error) {
	if path := os.Getenv("GEMINI_PROXY_USAGE_FILE"); path != "" {
		return path, nil
	}
	var base string
	switch runtime.GOOS {
	case "windows":
		base = os.Getenv("LOCALAPPDATA")
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, "Library", "Application Support")
	default:
		base = os.Getenv("XDG_STATE_HOME")
		if !filepath.IsAbs(base) {
			base = ""
		}
	}
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		if runtime.GOOS == "windows" {
			base = filepath.Join(home, "AppData", "Local")
		} else {
			base = filepath.Join(home, ".local", "state")
		}
	}
	return filepath.Join(base, "gemini-proxy", "usage.json"), nil
}

func NewUsageTracker(options ...UsageTrackerOption) *UsageTracker {
	t := &UsageTracker{now: time.Now}
	for _, option := range options {
		if option != nil {
			option(t)
		}
	}
	var pathErr error
	if !t.explicitFile {
		t.filePath, pathErr = defaultUsageFile()
	}
	if t.filePath != "" {
		t.filePath, pathErr = filepath.Abs(t.filePath)
	}
	t.stats = GlobalUsageStats{SchemaVersion: 1, TrackingStartedAt: t.now().UTC(), Models: make(map[string]*ModelUsage), Persistence: PersistenceStatus{Enabled: t.filePath != "", Healthy: true}}
	if pathErr != nil {
		t.stats.Persistence.Enabled = true
		t.blocked = true
		t.persistenceFailure(pathErr)
		return t
	}
	if t.filePath != "" {
		if err := os.MkdirAll(filepath.Dir(t.filePath), 0700); err != nil {
			t.blocked = true
			t.persistenceFailure(err)
			return t
		}
		lock, err := os.OpenFile(t.filePath+".lock", os.O_CREATE|os.O_RDWR, 0600)
		if err == nil {
			if err = lockUsageFile(lock); err != nil {
				_ = lock.Close()
			} else {
				t.lockFile = lock
			}
		}
		if err != nil {
			t.blocked = true
			t.persistenceFailure(fmt.Errorf("acquire usage ownership: %w", err))
			return t
		}
		if data, err := os.ReadFile(t.filePath); err == nil {
			var loaded GlobalUsageStats
			if err = decodeUsageState(data, &loaded); err == nil {
				err = validateUsageState(loaded)
			}
			if err != nil {
				t.blocked = true
				t.persistenceFailure(fmt.Errorf("load state: %w", err))
			} else {
				loaded.Persistence = PersistenceStatus{Enabled: true, Healthy: true}
				t.stats = loaded
			}
		} else if !os.IsNotExist(err) {
			t.blocked = true
			t.persistenceFailure(err)
		}
	}
	return t
}

// Record observes exactly one completed upstream result. Nil usage is unknown;
// a non-nil all-zero result is measured.
func (t *UsageTracker) Record(model string, usage *Usage) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return
	}
	delta := ModelUsage{CompletedRequests: 1}
	if usage == nil {
		delta.UnknownUsageRequests = 1
	} else {
		delta.MeasuredRequests = 1
		delta.PromptTokens = int64(usage.PromptTokens)
		delta.CompletionTokens = int64(usage.CompletionTokens)
		delta.TotalTokens = int64(usage.TotalTokens)
		if usage.PromptTokensDetails != nil {
			delta.CachedTokens = int64(usage.PromptTokensDetails.CachedTokens)
		}
		if usage.CompletionTokensDetails != nil {
			delta.ReasoningTokens = int64(usage.CompletionTokensDetails.ReasoningTokens)
		}
		if err := validateModelUsage(delta); err != nil {
			log.Printf("invalid measured usage; recording unknown usage: %v", err)
			delta = ModelUsage{CompletedRequests: 1, UnknownUsageRequests: 1}
		}
	}
	m := t.stats.Models[model]
	if m == nil {
		m = &ModelUsage{}
	}
	global, perModel := t.stats.ModelUsage, *m
	if !addUsage(&global, delta) || !addUsage(&perModel, delta) {
		// Never wrap token sums or write a globally/model-inconsistent snapshot.
		// Preserve completion accounting as unknown if measurement cannot fit.
		log.Printf("usage counter overflow; recording unknown usage")
		delta = ModelUsage{CompletedRequests: 1, UnknownUsageRequests: 1}
		global, perModel = t.stats.ModelUsage, *m
		if !addUsage(&global, delta) || !addUsage(&perModel, delta) {
			log.Printf("usage request counters exhausted; completion not recorded")
			return
		}
	}
	t.stats.ModelUsage = global
	*m = perModel
	t.stats.Models[model] = m
	now := t.now().UTC()
	if now.Before(t.stats.TrackingStartedAt) {
		now = t.stats.TrackingStartedAt
	}
	if now.Before(t.stats.LastCompletedAt) {
		now = t.stats.LastCompletedAt
	}
	t.stats.LastCompletedAt = now
	t.persistLocked()
}

func addUsage(to *ModelUsage, delta ModelUsage) bool {
	values := []*int64{&to.CompletedRequests, &to.MeasuredRequests, &to.UnknownUsageRequests, &to.PromptTokens, &to.CompletionTokens, &to.TotalTokens, &to.CachedTokens, &to.ReasoningTokens}
	increments := []int64{delta.CompletedRequests, delta.MeasuredRequests, delta.UnknownUsageRequests, delta.PromptTokens, delta.CompletionTokens, delta.TotalTokens, delta.CachedTokens, delta.ReasoningTokens}
	for i, v := range values {
		if increments[i] < 0 || *v > int64(^uint64(0)>>1)-increments[i] {
			return false
		}
	}
	for i, v := range values {
		*v += increments[i]
	}
	return true
}

func (t *UsageTracker) Stats() GlobalUsageStats {
	t.mu.RLock()
	defer t.mu.RUnlock()
	cp := t.stats
	cp.Models = make(map[string]*ModelUsage, len(t.stats.Models))
	for model, usage := range t.stats.Models {
		v := *usage
		cp.Models[model] = &v
	}
	return cp
}

// Decode strictly: absent and null counts must not turn silently into zeros.
func decodeUsageState(data []byte, into *GlobalUsageStats) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(into); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	counters := []string{"completed_requests", "measured_requests", "unknown_usage_requests", "prompt_tokens", "completion_tokens", "total_tokens", "cached_tokens", "reasoning_tokens"}
	require := func(fields map[string]json.RawMessage, names []string) error {
		for _, name := range names {
			if value := fields[name]; len(value) == 0 || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
				return fmt.Errorf("missing or null usage field %s", name)
			}
		}
		return nil
	}
	if err := require(fields, append(append([]string{}, counters...), "schema_version", "tracking_started_at", "last_completed_at", "models", "persistence")); err != nil {
		return err
	}
	var models map[string]map[string]json.RawMessage
	if err := json.Unmarshal(fields["models"], &models); err != nil {
		return err
	}
	for _, model := range models {
		if err := require(model, counters); err != nil {
			return err
		}
	}
	return nil
}

func validateModelUsage(m ModelUsage) error {
	if m.CompletedRequests < 0 || m.MeasuredRequests < 0 || m.UnknownUsageRequests < 0 || m.MeasuredRequests > m.CompletedRequests || m.UnknownUsageRequests != m.CompletedRequests-m.MeasuredRequests {
		return fmt.Errorf("invalid request counters")
	}
	if m.PromptTokens < 0 || m.CompletionTokens < 0 || m.TotalTokens < 0 || m.PromptTokens > m.TotalTokens || m.CompletionTokens != m.TotalTokens-m.PromptTokens || m.CachedTokens < 0 || m.CachedTokens > m.PromptTokens || m.ReasoningTokens < 0 || m.ReasoningTokens > m.CompletionTokens {
		return fmt.Errorf("invalid token counters")
	}
	if m.MeasuredRequests == 0 && m.TotalTokens != 0 {
		return fmt.Errorf("tokens without measured requests")
	}
	return nil
}

func validateUsageState(s GlobalUsageStats) error {
	if s.SchemaVersion != 1 {
		return fmt.Errorf("unsupported usage schema %d", s.SchemaVersion)
	}
	if s.Models == nil || s.TrackingStartedAt.IsZero() {
		return fmt.Errorf("missing usage state fields")
	}
	if s.CompletedRequests == 0 && !s.LastCompletedAt.IsZero() || s.CompletedRequests > 0 && (s.LastCompletedAt.IsZero() || s.LastCompletedAt.Before(s.TrackingStartedAt)) {
		return fmt.Errorf("invalid completion timestamps")
	}
	if err := validateModelUsage(s.ModelUsage); err != nil {
		return err
	}
	var sum ModelUsage
	for _, m := range s.Models {
		if m == nil {
			return fmt.Errorf("null model entry")
		}
		if err := validateModelUsage(*m); err != nil {
			return err
		}
		if !addUsage(&sum, *m) {
			return fmt.Errorf("model sum overflow")
		}
	}
	if sum != s.ModelUsage {
		return fmt.Errorf("model totals do not match global totals")
	}
	return nil
}

func (t *UsageTracker) persistenceFailure(err error) {
	t.stats.Persistence.Healthy = false
	t.stats.Persistence.Error = err.Error()
	log.Printf("usage persistence unavailable: %v", err)
}

// persistLocked runs under the accounting mutex, so an older snapshot can never
// overwrite a newer one. Files are flushed before a same-directory rename.
func (t *UsageTracker) persistLocked() {
	if t.filePath == "" || t.blocked {
		return
	}
	t.stats.Persistence = PersistenceStatus{Enabled: true, Healthy: true}
	if err := t.save(); err != nil {
		t.persistenceFailure(err)
	}
}

func (t *UsageTracker) save() error {
	dir := filepath.Dir(t.filePath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".usage-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err = f.Chmod(0600); err != nil {
		return err
	}
	if err = json.NewEncoder(f).Encode(t.stats); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = replaceUsageFile(f.Name(), t.filePath); err != nil {
		return err
	}
	if err = syncUsageDirectory(dir); err != nil {
		return fmt.Errorf("sync state directory: %w", err)
	}
	return nil
}

// Close releases process ownership. Record calls after Close are ignored.
func (t *UsageTracker) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return t.closeErr
	}
	t.persistLocked()
	t.closed = true
	if !t.stats.Persistence.Healthy {
		t.closeErr = errors.New(t.stats.Persistence.Error)
	}
	if t.lockFile != nil {
		t.closeErr = errors.Join(t.closeErr, unlockUsageFile(t.lockFile))
		t.lockFile = nil
	}
	return t.closeErr
}

func (s *Server) handleProxyUsage(w http.ResponseWriter, r *http.Request) {
	if s.tracker == nil {
		http.Error(w, "usage tracker not configured", http.StatusNotFound)
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(s.tracker.Stats())
}
