package worker

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"
)

const (
	DefaultModel = "gemini-3.8-flash-high"
	IdleTimeout  = 15 * time.Minute
)

// NormalizeModel resolves arbitrary model names / aliases into canonical agy models.
func NormalizeModel(input string) string {
	raw := strings.TrimSpace(strings.ToLower(input))
	if raw == "" || raw == "default" || raw == "gemini" {
		return DefaultModel
	}

	// Remove common provider prefixes
	for _, p := range []string{"google/", "gemini/", "custom/", "openai/"} {
		raw = strings.TrimPrefix(raw, p)
	}

	// Normalize spaces and underscores to dashes
	raw = strings.ReplaceAll(raw, " ", "-")
	raw = strings.ReplaceAll(raw, "_", "-")

	// Direct matches
	switch raw {
	case "gemini-3.8-flash", "gemini-3.8-flash-high", "flash", "flash-high", "gemini-flash":
		return "gemini-3.8-flash-high"
	case "gemini-3.8-flash-medium", "flash-medium":
		return "gemini-3.8-flash-medium"
	case "gemini-3.8-flash-low", "flash-low":
		return "gemini-3.8-flash-low"
	case "gemini-3.7-flash", "gemini-3.7-flash-high":
		return "gemini-3.7-flash-high"
	case "gemini-3.7-flash-medium":
		return "gemini-3.7-flash-medium"
	case "gemini-3.7-flash-low":
		return "gemini-3.7-flash-low"
	case "gemini-3.1-pro", "gemini-3.1-pro-high", "pro", "pro-high", "gemini-pro":
		return "gemini-3.1-pro-high"
	case "gemini-3.1-pro-low", "pro-low":
		return "gemini-3.1-pro-low"
	case "gemini-3.6-flash", "gemini-3.6-flash-high":
		return "gemini-3.6-flash-high"
	}

	// Substring heuristics for Gemini
	if strings.Contains(raw, "pro") {
		return "gemini-3.1-pro-high"
	}
	if strings.Contains(raw, "3.7") {
		return "gemini-3.7-flash-high"
	}
	if strings.Contains(raw, "flash") || strings.Contains(raw, "gemini") {
		return "gemini-3.8-flash-high"
	}

	// Fallback to default
	return DefaultModel
}

// WorkerPool manages a pool of model workers with idle shutdown.
type WorkerPool struct {
	mu      sync.Mutex
	workers map[string]*Worker
	stopCh  chan struct{}
}

// NewWorkerPool initializes the pool and starts the idle reaper.
func NewWorkerPool() *WorkerPool {
	p := &WorkerPool{
		workers: make(map[string]*Worker),
		stopCh:  make(chan struct{}),
	}
	go p.reapLoop()
	return p
}

// GetWorker returns an active worker for the model, spinning one up if needed.
func (p *WorkerPool) GetWorker(ctx context.Context, modelName string) (*Worker, error) {
	canonical := NormalizeModel(modelName)

	p.mu.Lock()
	w, exists := p.workers[canonical]
	if exists && w.IsHealthy() {
		p.mu.Unlock()
		return w, nil
	}
	p.mu.Unlock()

	// Launch new worker outside mutex lock to avoid blocking other models
	newW, err := NewWorker(canonical, func() {
		p.mu.Lock()
		delete(p.workers, canonical)
		p.mu.Unlock()
	})
	if err != nil {
		return nil, err
	}

	p.mu.Lock()
	// Check if another worker was registered while we were starting
	if existing, ok := p.workers[canonical]; ok && existing.IsHealthy() {
		p.mu.Unlock()
		newW.Close()
		return existing, nil
	}
	p.workers[canonical] = newW
	p.mu.Unlock()

	return newW, nil
}

// reapLoop periodically shuts down workers that have been idle past IdleTimeout.
func (p *WorkerPool) reapLoop() {
	ticker := time.NewTicker(2 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-p.stopCh:
			return
		case <-ticker.C:
			p.mu.Lock()
			now := time.Now()
			for model, w := range p.workers {
				if !w.IsHealthy() {
					delete(p.workers, model)
					w.Close()
					continue
				}
				if now.Sub(w.LastUsed()) > IdleTimeout {
					log.Printf("[WorkerPool] Pruning idle worker for %s (idle for %v)", model, now.Sub(w.LastUsed()).Round(time.Second))
					delete(p.workers, model)
					w.Close()
				}
			}
			p.mu.Unlock()
		}
	}
}

// CloseAll closes all active workers and stops the reaper.
func (p *WorkerPool) CloseAll() {
	close(p.stopCh)
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, w := range p.workers {
		w.Close()
	}
	p.workers = make(map[string]*Worker)
}
