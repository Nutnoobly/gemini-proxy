package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func newTestUsageTracker(t *testing.T) *UsageTracker {
	t.Helper()
	tracker := NewUsageTracker(WithUsageFile(filepath.Join(t.TempDir(), "state", "usage.json")))
	t.Cleanup(func() {
		if err := tracker.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return tracker
}

func TestUsageTrackerAccounting(t *testing.T) {
	now := time.Date(2026, 10, 16, 1, 2, 3, 0, time.UTC)
	tracker := NewUsageTracker(WithUsageFile(filepath.Join(t.TempDir(), "usage.json")), WithUsageClock(func() time.Time { return now }))
	t.Cleanup(func() { _ = tracker.Close() })
	now = now.Add(time.Minute)
	tracker.Record("model-a", &Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15, PromptTokensDetails: &PromptTokensDetails{CachedTokens: 4}, CompletionTokensDetails: &CompletionTokensDetails{ReasoningTokens: 2}})
	tracker.Record("model-a", nil)
	tracker.Record("model-b", &Usage{})
	got := tracker.Stats()
	if got.SchemaVersion != 1 || got.CompletedRequests != 3 || got.MeasuredRequests != 2 || got.UnknownUsageRequests != 1 || got.PromptTokens != 10 || got.CompletionTokens != 5 || got.TotalTokens != 15 || got.CachedTokens != 4 || got.ReasoningTokens != 2 {
		t.Fatalf("unexpected totals: %+v", got)
	}
	if got.Models["model-a"].CompletedRequests != 2 || got.Models["model-a"].UnknownUsageRequests != 1 || got.Models["model-b"].MeasuredRequests != 1 {
		t.Fatalf("unexpected models: %+v", got.Models)
	}
	if !got.TrackingStartedAt.Equal(now.Add(-time.Minute)) || !got.LastCompletedAt.Equal(now) {
		t.Fatalf("wrong timestamps: %+v", got)
	}
	got.Models["model-a"].PromptTokens = 999
	delete(got.Models, "model-b")
	if snapshot := tracker.Stats(); snapshot.Models["model-a"].PromptTokens != 10 || snapshot.Models["model-b"] == nil {
		t.Fatal("Stats leaked mutable state")
	}
}

func TestUsageTrackerInvalidStateIsReadOnly(t *testing.T) {
	valid := GlobalUsageStats{SchemaVersion: 1, TrackingStartedAt: time.Now().UTC(), Models: map[string]*ModelUsage{}, Persistence: PersistenceStatus{Enabled: true, Healthy: true}}
	baseline, err := json.Marshal(valid)
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"missing_counter": strings.Replace(string(baseline), `"prompt_tokens":0,`, ``, 1),
		"null_counter":    strings.Replace(string(baseline), `"prompt_tokens":0`, `"prompt_tokens":null`, 1),
		"unknown_field":   strings.Replace(string(baseline), `"schema_version":1`, `"unexpected":42,"schema_version":1`, 1),
		"truncated":       "{", "null": "null", "empty": "", "unsupported": strings.Replace(string(baseline), `"schema_version":1`, `"schema_version":2`, 1),
		"null_models":    strings.Replace(string(baseline), `"models":{}`, `"models":null`, 1),
		"null_model":     strings.Replace(string(baseline), `"models":{}`, `"models":{"a":null}`, 1),
		"missing_fields": `{"schema_version":1,"models":{}}`,
		"negative":       strings.Replace(string(baseline), `"completed_requests":0`, `"completed_requests":-1`, 1),
		"bad_count":      strings.Replace(string(baseline), `"completed_requests":0`, `"completed_requests":1`, 1),
		"bad_tokens":     strings.Replace(string(baseline), `"total_tokens":0`, `"total_tokens":1`, 1),
		"bad_subset":     strings.Replace(string(baseline), `"cached_tokens":0`, `"cached_tokens":1`, 1),
		"model_mismatch": strings.Replace(string(baseline), `"models":{}`, `"models":{"a":{"completed_requests":1,"measured_requests":1}}`, 1),
		"trailing_json":  string(baseline) + ` {}`,
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "usage.json")
			if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
				t.Fatal(err)
			}
			tracker := NewUsageTracker(WithUsageFile(path))
			defer tracker.Close()
			before := tracker.Stats()
			if before.Persistence.Healthy || before.Persistence.Error == "" || !before.Persistence.Enabled {
				t.Fatalf("invalid state not reported: %+v", before)
			}
			tracker.Record("fresh", &Usage{})
			if got := tracker.Stats(); got.CompletedRequests != 1 || got.Models["fresh"] == nil || got.Persistence.Healthy {
				t.Fatalf("accounting after corrupt load: %+v", got)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(data) != raw {
				t.Fatal("corrupt state was overwritten")
			}
		})
	}
}

func TestUsageTrackerExclusiveOwnership(t *testing.T) {
	if path := os.Getenv("GEMINI_PROXY_USAGE_LOCK_TEST"); path != "" {
		tracker := NewUsageTracker(WithUsageFile(path))
		defer tracker.Close()
		if got := tracker.Stats().Persistence; got.Healthy || got.Error == "" {
			t.Fatalf("second process acquired state: %+v", got)
		}
		tracker.Record("child", &Usage{})
		return
	}
	path := filepath.Join(t.TempDir(), "usage.json")
	owner := NewUsageTracker(WithUsageFile(path))
	defer owner.Close()
	owner.Record("owner", &Usage{})
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	second := NewUsageTracker(WithUsageFile(path))
	if got := second.Stats().Persistence; got.Healthy || got.Error == "" {
		t.Fatalf("second owner accepted: %+v", got)
	}
	second.Record("second", &Usage{})
	_ = second.Close()
	cmd := exec.Command(os.Args[0], "-test.run=^TestUsageTrackerExclusiveOwnership$")
	cmd.Env = append(os.Environ(), "GEMINI_PROXY_USAGE_LOCK_TEST="+path)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("child: %v: %s", err, output)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("non-owner overwrote state")
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	if err := owner.Close(); err != nil {
		t.Fatalf("close not idempotent: %v", err)
	}
	owner.Record("after-close", &Usage{})
	if owner.Stats().CompletedRequests != 1 {
		t.Fatal("closed tracker accepted record")
	}
	next := NewUsageTracker(WithUsageFile(path))
	defer next.Close()
	if got := next.Stats(); !got.Persistence.Healthy || got.CompletedRequests != 1 {
		t.Fatalf("lock not released: %+v", got)
	}
}

func TestUsageTrackerInvalidMeasurementsBecomeUnknown(t *testing.T) {
	cases := []*Usage{
		{PromptTokens: -1, CompletionTokens: 1, TotalTokens: 0},
		{PromptTokens: 1, CompletionTokens: 2, TotalTokens: 4},
		{PromptTokens: 1, TotalTokens: 1, PromptTokensDetails: &PromptTokensDetails{CachedTokens: 2}},
		{CompletionTokens: 1, TotalTokens: 1, CompletionTokensDetails: &CompletionTokensDetails{ReasoningTokens: -1}},
		{CompletionTokens: 1, TotalTokens: 1, CompletionTokensDetails: &CompletionTokensDetails{ReasoningTokens: 2}},
	}
	tracker := NewUsageTracker(WithUsageFile(""))
	defer tracker.Close()
	for _, usage := range cases {
		tracker.Record("invalid", usage)
	}
	got := tracker.Stats()
	if got.CompletedRequests != int64(len(cases)) || got.UnknownUsageRequests != int64(len(cases)) || got.MeasuredRequests != 0 || got.TotalTokens != 0 || got.PromptTokens != 0 || got.CachedTokens != 0 {
		t.Fatalf("invalid measurement counted: %+v", got)
	}
}

func TestUsageTrackerPersistenceFailureAndCloseRetry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.json")
	tracker := NewUsageTracker(WithUsageFile(path))
	defer tracker.Close()
	tracker.Record("a", &Usage{})
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path, path+".before"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	tracker.Record("a", &Usage{PromptTokens: 2, TotalTokens: 2})
	got := tracker.Stats()
	if got.Persistence.Healthy || got.Persistence.Error == "" || got.CompletedRequests != 2 || got.TotalTokens != 2 {
		t.Fatalf("write failure affected accounting: %+v", got)
	}
	old, err := os.ReadFile(path + ".before")
	if err != nil || string(old) != string(original) {
		t.Fatal("original state damaged")
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".usage-") {
			t.Fatalf("temporary file leaked: %s", entry.Name())
		}
	}
	if err := os.Rename(path, path+".obstruction"); err != nil {
		t.Fatal(err)
	}
	if err := tracker.Close(); err != nil {
		t.Fatalf("final retry failed: %v", err)
	}
	reloaded := NewUsageTracker(WithUsageFile(path))
	defer reloaded.Close()
	if got := reloaded.Stats(); !got.Persistence.Healthy || got.CompletedRequests != 2 || got.TotalTokens != 2 {
		t.Fatalf("Close did not persist pending state: %+v", got)
	}
}

func TestUsageTrackerDefaultPathAndOverride(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "xdg"))
	t.Setenv("LOCALAPPDATA", filepath.Join(root, "windows"))
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv("USERPROFILE", filepath.Join(root, "home"))
	t.Setenv("GEMINI_PROXY_USAGE_FILE", "")
	var expected string
	switch runtime.GOOS {
	case "windows":
		expected = filepath.Join(root, "windows", "gemini-proxy", "usage.json")
	case "darwin":
		expected = filepath.Join(root, "home", "Library", "Application Support", "gemini-proxy", "usage.json")
	default:
		expected = filepath.Join(root, "xdg", "gemini-proxy", "usage.json")
	}
	tracker := NewUsageTracker()
	tracker.Record("default", nil)
	if err := tracker.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(expected); err != nil {
		t.Fatalf("default state missing: %s: %v", expected, err)
	}
	override := filepath.Join(root, "override", "usage.json")
	t.Setenv("GEMINI_PROXY_USAGE_FILE", override)
	configured := NewUsageTracker()
	configured.Record("configured", nil)
	if err := configured.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(override); err != nil {
		t.Fatal(err)
	}
	disabled := NewUsageTracker(WithUsageFile(""))
	disabled.Record("memory", nil)
	if got := disabled.Stats().Persistence; got.Enabled || !got.Healthy {
		t.Fatalf("explicit disabled ignored: %+v", got)
	}
	if err := disabled.Close(); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && runtime.GOOS != "darwin" {
		t.Setenv("GEMINI_PROXY_USAGE_FILE", "")
		t.Setenv("XDG_STATE_HOME", "relative-is-invalid")
		fallback := NewUsageTracker()
		fallback.Record("fallback", nil)
		if err := fallback.Close(); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(root, "home", ".local", "state", "gemini-proxy", "usage.json")); err != nil {
			t.Fatal(err)
		}
	}
}

func TestUsageTrackerReadOnlyEndpoints(t *testing.T) {
	tracker := newTestUsageTracker(t)
	tracker.Record("api", nil)
	server := &Server{tracker: tracker}
	before := tracker.Stats()
	original, err := os.ReadFile(tracker.filePath)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/v1/usage", "/usage"} {
		for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch, http.MethodHead, http.MethodOptions} {
			w := httptest.NewRecorder()
			server.Routes().ServeHTTP(w, httptest.NewRequest(method, path, nil))
			if method == http.MethodGet {
				if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "application/json" || w.Header().Get("Cache-Control") != "no-store" {
					t.Fatalf("bad GET response: %d %v", w.Code, w.Header())
				}
				var got GlobalUsageStats
				if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(got, before) {
					t.Fatalf("inconsistent HTTP snapshot: %+v", got)
				}
			} else if w.Code != http.StatusMethodNotAllowed || w.Header().Get("Allow") != http.MethodGet {
				t.Fatalf("%s %s: got %d Allow=%q", method, path, w.Code, w.Header().Get("Allow"))
			}
		}
	}
	after, err := os.ReadFile(tracker.filePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(original) || !reflect.DeepEqual(tracker.Stats(), before) {
		t.Fatal("endpoint mutated usage")
	}
}

func TestUsageTrackerOverflowAndBackwardClock(t *testing.T) {
	now := time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "usage.json")
	max := int64(^uint64(0) >> 1)
	usage := ModelUsage{CompletedRequests: 1, MeasuredRequests: 1, PromptTokens: max, TotalTokens: max}
	initial := GlobalUsageStats{SchemaVersion: 1, ModelUsage: usage, TrackingStartedAt: now, LastCompletedAt: now, Models: map[string]*ModelUsage{"a": &usage}}
	data, err := json.Marshal(initial)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	tracker := NewUsageTracker(WithUsageFile(path), WithUsageClock(func() time.Time { return now.Add(-time.Hour) }))
	defer tracker.Close()
	tracker.Record("a", &Usage{PromptTokens: 1, TotalTokens: 1})
	got := tracker.Stats()
	if got.CompletedRequests != 2 || got.MeasuredRequests != 1 || got.UnknownUsageRequests != 1 || got.TotalTokens != max || got.Models["a"].UnknownUsageRequests != 1 {
		t.Fatalf("overflow corrupted accounting: %+v", got)
	}
	if !got.LastCompletedAt.Equal(now) {
		t.Fatalf("clock rollback created invalid timestamp: %v", got.LastCompletedAt)
	}
	if err := validateUsageState(got); err != nil {
		t.Fatal(err)
	}
}

func TestUsageTrackerDurableRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private", "usage.json")
	tracker := NewUsageTracker(WithUsageFile(path))
	tracker.Record("a", &Usage{PromptTokens: 12, CompletionTokens: 3, TotalTokens: 15})
	tracker.Record("b", nil)
	before := tracker.Stats()
	if !before.Persistence.Enabled || !before.Persistence.Healthy || before.Persistence.Error != "" {
		t.Fatalf("persistence unavailable: %+v", before.Persistence)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var disk GlobalUsageStats
	if err := json.Unmarshal(data, &disk); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, disk) {
		t.Fatalf("disk differs: %s", data)
	}
	for file, mode := range map[string]os.FileMode{path: 0600, filepath.Dir(path): 0700} {
		info, err := os.Stat(file)
		if err != nil {
			t.Fatal(err)
		}
		if runtime.GOOS != "windows" && info.Mode().Perm() != mode {
			t.Fatalf("%s permissions %o, want %o", file, info.Mode().Perm(), mode)
		}
	}
	if err := tracker.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := NewUsageTracker(WithUsageFile(path))
	t.Cleanup(func() { _ = reopened.Close() })
	if got := reopened.Stats(); !reflect.DeepEqual(before, got) {
		t.Fatalf("restart lost state: before=%+v after=%+v", before, got)
	}
	reopened.Record("a", &Usage{})
	if reopened.Stats().CompletedRequests != 3 {
		t.Fatal("restart did not continue accounting")
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() != "usage.json" && entry.Name() != "usage.json.lock" {
			t.Fatalf("left temporary state: %s", entry.Name())
		}
	}
}

func TestUsageTrackerConcurrentSnapshots(t *testing.T) {
	tracker := newTestUsageTracker(t)
	var writers sync.WaitGroup
	for i := 0; i < 8; i++ {
		writers.Add(1)
		go func() {
			defer writers.Done()
			for j := 0; j < 10; j++ {
				tracker.Record("m", &Usage{PromptTokens: 2, CompletionTokens: 1, TotalTokens: 3})
				if err := validateUsageState(tracker.Stats()); err != nil {
					t.Error(err)
				}
			}
		}()
	}
	writers.Wait()
	got := tracker.Stats()
	if got.CompletedRequests != 80 || got.MeasuredRequests != 80 || got.TotalTokens != 240 {
		t.Fatalf("concurrent accounting lost updates: %+v", got)
	}
}
