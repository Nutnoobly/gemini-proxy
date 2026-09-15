package worker

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"
)

// Worker represents a persistent agy subprocess.
type Worker struct {
	model        string
	cmd          *exec.Cmd
	stdin        io.WriteCloser
	scanner      *bufio.Scanner
	mu           sync.Mutex
	turnMu       sync.Mutex
	isHealthy    atomic.Bool
	lastUsed     time.Time
	initPayload  *InitPayload
	onDead       func()
}

// NewWorker starts a new agy subprocess for the specified model.
func NewWorker(model string, onDead func()) (*Worker, error) {
	// agy command flags
	args := []string{
		"--model", model,
		"--input-format", "stream-json",
		"--output-format", "stream-json",
		"--dangerously-skip-permissions",
		"-p", "",
	}

	cmd := exec.Command("agy", args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to open stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, fmt.Errorf("failed to open stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		stdin.Close()
		stdout.Close()
		return nil, fmt.Errorf("failed to start agy: %w", err)
	}

	scanner := bufio.NewScanner(stdout)
	// Give scanner a large buffer (10MB) to handle large tool outputs/prompts
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, 10*1024*1024)

	w := &Worker{
		model:     model,
		cmd:       cmd,
		stdin:     stdin,
		scanner:   scanner,
		lastUsed:  time.Now(),
		onDead:    onDead,
	}
	w.isHealthy.Store(true)

	// Read until "init" event is received (with 20s timeout)
	initCh := make(chan error, 1)
	go func() {
		for scanner.Scan() {
			line := scanner.Bytes()
			if len(line) == 0 {
				continue
			}

			var evt StreamOutputEvent
			if err := json.Unmarshal(line, &evt); err != nil {
				continue
			}

			if evt.Event == "init" && evt.Init != nil {
				w.initPayload = evt.Init
				initCh <- nil
				return
			}

			if evt.Event == "result" && evt.Result != nil && evt.Result.Status == "ERROR" {
				initCh <- fmt.Errorf("agy startup error: %s", evt.Result.Error)
				return
			}
		}
		if err := scanner.Err(); err != nil {
			initCh <- fmt.Errorf("error reading startup stream: %w", err)
		} else {
			initCh <- errors.New("agy exited before sending init event")
		}
	}()

	select {
	case err := <-initCh:
		if err != nil {
			w.Close()
			return nil, err
		}
	case <-time.After(25 * time.Second):
		w.Close()
		return nil, errors.New("timeout waiting for agy init event")
	}

	log.Printf("[Worker %s] Started successfully (PID: %d)", model, cmd.Process.Pid)
	return w, nil
}

// Model returns the worker's model slug.
func (w *Worker) Model() string {
	return w.model
}

// IsHealthy returns whether the worker is alive and functioning.
func (w *Worker) IsHealthy() bool {
	return w.isHealthy.Load()
}

// LastUsed returns the time when the worker was last used.
func (w *Worker) LastUsed() time.Time {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.lastUsed
}

// SendPrompt sends a prompt to agy and invokes onDelta for every text delta.
// It returns the final ResultPayload when the turn is completed.
func (w *Worker) SendPrompt(ctx context.Context, prompt string, onDelta func(string)) (*ResultPayload, error) {
	if !w.isHealthy.Load() {
		return nil, errors.New("worker is not healthy")
	}

	w.turnMu.Lock()
	defer w.turnMu.Unlock()

	w.mu.Lock()
	w.lastUsed = time.Now()
	w.mu.Unlock()

	msg := StreamInputMessage{
		Event: "user",
		Message: StreamInputUserMessage{
			Content: prompt,
		},
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal turn message: %w", err)
	}
	data = append(data, '\n')

	if _, err := w.stdin.Write(data); err != nil {
		w.markDead()
		return nil, fmt.Errorf("failed to write to agy stdin: %w", err)
	}

	type turnResult struct {
		res *ResultPayload
		err error
	}
	resultCh := make(chan turnResult, 1)

	go func() {
		for w.scanner.Scan() {
			line := w.scanner.Bytes()
			if len(line) == 0 {
				continue
			}

			var evt StreamOutputEvent
			if err := json.Unmarshal(line, &evt); err != nil {
				log.Printf("[Worker %s] failed to unmarshal line: %v (line: %s)", w.model, err, string(line))
				continue
			}

			switch evt.Event {
			case "step_update":
				if evt.StepUpdate != nil && evt.StepUpdate.StepType == "agent_response" && evt.StepUpdate.TextDelta != "" {
					if onDelta != nil {
						onDelta(evt.StepUpdate.TextDelta)
					}
				}
			case "result":
				if evt.Result != nil {
					if evt.Result.Status == "ERROR" {
						resultCh <- turnResult{nil, fmt.Errorf("agy error: %s", evt.Result.Error)}
					} else {
						resultCh <- turnResult{evt.Result, nil}
					}
					return
				}
			}
		}

		if err := w.scanner.Err(); err != nil {
			resultCh <- turnResult{nil, fmt.Errorf("stream read error: %w", err)}
		} else {
			resultCh <- turnResult{nil, errors.New("agy exited unexpectedly during turn")}
		}
	}()

	select {
	case <-ctx.Done():
		w.markDead()
		return nil, ctx.Err()
	case res := <-resultCh:
		if res.err != nil {
			w.markDead()
			return nil, res.err
		}
		w.mu.Lock()
		w.lastUsed = time.Now()
		w.mu.Unlock()
		return res.res, nil
	}
}

func (w *Worker) markDead() {
	if w.isHealthy.CompareAndSwap(true, false) {
		log.Printf("[Worker %s] Marked as dead", w.model)
		if w.cmd != nil && w.cmd.Process != nil {
			_ = w.cmd.Process.Kill()
		}
		if w.stdin != nil {
			_ = w.stdin.Close()
		}
		if w.onDead != nil {
			w.onDead()
		}
	}
}

// Close terminates the worker process.
func (w *Worker) Close() {
	w.isHealthy.Store(false)
	if w.stdin != nil {
		_ = w.stdin.Close()
	}
	if w.cmd != nil && w.cmd.Process != nil {
		_ = w.cmd.Process.Kill()
		_ = w.cmd.Wait()
	}
}
