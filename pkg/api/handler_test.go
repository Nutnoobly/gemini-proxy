package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"gemini-proxy/pkg/worker"
)

func TestHandleModels(t *testing.T) {
	pool := worker.NewWorkerPool()
	defer pool.CloseAll()
	server := NewServer(pool, WithUsageFile(""))
	t.Cleanup(func() { _ = server.Close() })

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	w := httptest.NewRecorder()

	server.Routes().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var list ModelList
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatalf("failed to unmarshal models response: %v", err)
	}

	foundArgon := false
	for _, m := range list.Data {
		if m.ID == "gemini-4-argon-high" {
			foundArgon = true
			break
		}
	}

	if !foundArgon {
		t.Errorf("gemini-4-argon-high not found in /v1/models response")
	}
}

func TestHandleModelDetail(t *testing.T) {
	pool := worker.NewWorkerPool()
	defer pool.CloseAll()
	server := NewServer(pool, WithUsageFile(""))
	t.Cleanup(func() { _ = server.Close() })

	req := httptest.NewRequest(http.MethodGet, "/v1/models/gemini-4-argon", nil)
	w := httptest.NewRecorder()

	server.Routes().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var card ModelCard
	if err := json.Unmarshal(w.Body.Bytes(), &card); err != nil {
		t.Fatalf("failed to unmarshal model detail response: %v", err)
	}

	if card.ID != "gemini-4-argon-high" {
		t.Errorf("expected canonical model gemini-4-argon-high, got %s", card.ID)
	}
}
