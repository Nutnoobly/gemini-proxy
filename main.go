package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"gemini-proxy/pkg/api"
	"gemini-proxy/pkg/service"
	"gemini-proxy/pkg/worker"
)

const banner = `
   ____               _       _ ____                      
  / ___| ___ _ __ ___(_)_ __ (_)  _ \ _ __ _____  ___   _ 
 | |  _ / _ \ '_ ` + "`" + ` _ \ | '_ \| | |_) | '__/ _ \ \/ / | | |
 | |_| |  __/ | | | | | | | | |  __/| | | (_) >  <| |_| |
  \____|\___|_| |_| |_|_|_| |_|_|_|   |_|  \___/_/\_\\__, |
                                                      |___/ 
 Google Gemini CLI Proxy for Hermes Agent
`

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "serve":
			runServe(os.Args[2:])
			return
		case "setup-hermes":
			runSetupHermes(os.Args[2:])
			return
		case "service":
			runService(os.Args[2:])
			return
		case "test":
			runTest(os.Args[2:])
			return
		case "--help", "-h", "help":
			printUsage()
			return
		}
	}

	// Default to serve if no subcommand provided
	runServe(os.Args[1:])
}

func printUsage() {
	fmt.Print(banner)
	fmt.Println("Usage: gemini-proxy <command> [options]")
	fmt.Println("\nCommands:")
	fmt.Println("  serve         Start the OpenAI-compatible proxy server (default)")
	fmt.Println("                --host <ip>          Host interface to bind (default: 127.0.0.1)")
	fmt.Println("                --port <port>        Port to listen on (default: 8080)")
	fmt.Println("                --idle-timeout <dur> Auto-shutdown after inactivity (default: 10m, 0 to disable)")
	fmt.Println("                --watch-hermes       Auto-shutdown when Hermes Agent closes (default: true)")
	fmt.Println("  setup-hermes  Configure ~/.hermes/config.yaml to use GeminiProxy")
	fmt.Println("  service       Manage systemd background service (install, status, stop, restart)")
	fmt.Println("                --port <port>        Service port (default: 8080)")
	fmt.Println("                --idle-timeout <dur> Service idle timeout (default: 10m)")
	fmt.Println("  test          Send a test request to verify proxy operation")
	fmt.Println("\nRun 'gemini-proxy <command> --help' for command-specific flags.")
}

func getListener(addr string) (net.Listener, error) {
	// Check for systemd socket activation
	if pidStr := os.Getenv("LISTEN_PID"); pidStr == strconv.Itoa(os.Getpid()) {
		if fdsStr := os.Getenv("LISTEN_FDS"); fdsStr != "" {
			if fds, err := strconv.Atoi(fdsStr); err == nil && fds >= 1 {
				log.Printf("[Main] Using systemd socket activation (FD 3)")
				file := os.NewFile(3, "systemd-socket")
				l, err := net.FileListener(file)
				_ = file.Close()
				if err != nil {
					return nil, fmt.Errorf("failed to adopt systemd socket: %w", err)
				}
				return l, nil
			}
		}
	}
	return net.Listen("tcp", addr)
}

func runServe(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	host := fs.String("host", "127.0.0.1", "Host interface to bind")
	port := fs.Int("port", 8080, "Port to listen on")
	idleTimeout := fs.Duration("idle-timeout", 10*time.Minute, "Auto-shutdown after period of inactivity (e.g. 5m, 10m, 0 to disable)")
	watchHermes := fs.Bool("watch-hermes", true, "Automatically shut down proxy when Hermes Agent closes")
	_ = fs.Parse(args)

	fmt.Print(banner)
	log.Printf("[Main] Initializing GeminiProxy on %s:%d...", *host, *port)
	if *idleTimeout > 0 {
		log.Printf("[Main] Idle auto-shutdown enabled: server will exit after %v with no requests", *idleTimeout)
	} else {
		log.Printf("[Main] Idle auto-shutdown disabled: server will run indefinitely")
	}

	workerIdle := 5 * time.Minute
	if *idleTimeout > 0 && *idleTimeout < workerIdle {
		workerIdle = *idleTimeout
	}
	pool := worker.NewWorkerPoolWithTimeout(workerIdle)
	server := api.NewServer(pool)

	addr := fmt.Sprintf("%s:%d", *host, *port)
	listener, err := getListener(addr)
	if err != nil {
		log.Fatalf("[Main] Failed to bind/adopt listener on %s: %v", addr, err)
	}

	httpServer := &http.Server{
		Handler: server.Routes(),
	}

	// Graceful shutdown channels
	stopCh := make(chan os.Signal, 1)
	signal.Notify(stopCh, os.Interrupt, syscall.SIGTERM)
	idleShutdownCh := make(chan struct{})

	// Hermes process lifecycle watcher
	if *watchHermes {
		log.Println("[Main] Hermes watcher enabled: will shut down automatically when Hermes closes")
		hermesWatcher := service.NewHermesWatcher(func() {
			select {
			case <-idleShutdownCh:
			default:
				close(idleShutdownCh)
			}
		})
		hermesWatcher.Start()
		defer hermesWatcher.Stop()

		server.SetOnActivity(func() {
			hermesWatcher.Arm()
		})
	}

	// Idle auto-shutdown watcher
	if *idleTimeout > 0 {
		go func() {
			checkInterval := 10 * time.Second
			if *idleTimeout < 10*time.Second {
				checkInterval = time.Second
			}
			ticker := time.NewTicker(checkInterval)
			defer ticker.Stop()

			for {
				select {
				case <-stopCh:
					return
				case <-idleShutdownCh:
					return
				case <-ticker.C:
					if server.ActiveRequests() == 0 {
						idleDuration := time.Since(server.LastActivity())
						if idleDuration >= *idleTimeout {
							log.Printf("[Main] Inactive for %v (limit %v). Shutting down automatically...",
								idleDuration.Round(time.Second), *idleTimeout)
							close(idleShutdownCh)
							return
						}
					}
				}
			}
		}()
	}

	go func() {
		log.Printf("[Main] Listening for Hermes connections on http://%s/v1", addr)
		log.Printf("[Main] Models endpoint available at http://%s/v1/models", addr)
		if err := httpServer.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.Fatalf("[Main] Server failure: %v", err)
		}
	}()

	select {
	case sig := <-stopCh:
		log.Printf("[Main] Received signal %v. Shutting down gracefully...", sig)
	case <-idleShutdownCh:
		log.Println("[Main] Auto-shutdown triggered due to inactivity. Shutting down gracefully...")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_ = httpServer.Shutdown(ctx)
	pool.CloseAll()
	log.Println("[Main] All workers stopped. Goodbye!")
}

func runSetupHermes(args []string) {
	fs := flag.NewFlagSet("setup-hermes", flag.ExitOnError)
	baseURL := fs.String("base-url", "http://127.0.0.1:8080/v1", "Proxy base URL for Hermes")
	model := fs.String("model", "gemini-3.8-flash-high", "Default model to configure in Hermes")
	_ = fs.Parse(args)

	log.Printf("[Setup] Configuring Hermes Agent...")
	if err := service.ConfigureHermes(*baseURL, *model); err != nil {
		log.Fatalf("[Setup] Failed: %v", err)
	}
	fmt.Printf("\n✓ Hermes Agent is now configured to use GeminiProxy at %s (model: %s)\n", *baseURL, *model)
	fmt.Println("You can now start Hermes with: hermes chat -q \"Hello!\"")
}

func runService(args []string) {
	if len(args) == 0 {
		fmt.Println("Usage: gemini-proxy service <install|status|stop|restart> [--port 8080] [--idle-timeout 10m]")
		return
	}

	sub := args[0]
	fs := flag.NewFlagSet("service", flag.ExitOnError)
	port := fs.Int("port", 8080, "Port for service")
	idleTimeout := fs.String("idle-timeout", "10m", "Idle duration before auto-shutdown (e.g. 5m, 10m, or 0 to disable)")
	_ = fs.Parse(args[1:])

	exe, err := os.Executable()
	if err != nil {
		log.Fatalf("Could not determine executable path: %v", err)
	}

	switch sub {
	case "install":
		if err := service.InstallService(exe, *port, *idleTimeout); err != nil {
			log.Fatalf("Installation failed: %v", err)
		}
		fmt.Printf("\n✓ Service & socket installed with on-demand activation and %s idle auto-shutdown.\n", *idleTimeout)
		fmt.Println("Check status with: gemini-proxy service status")
	case "status":
		_ = service.StatusService()
	case "stop":
		if err := service.StopService(); err != nil {
			log.Fatalf("Stop failed: %v", err)
		}
	case "restart":
		if err := service.RestartService(); err != nil {
			log.Fatalf("Restart failed: %v", err)
		}
	default:
		fmt.Printf("Unknown service action: %s. Use install, status, stop, or restart.\n", sub)
	}
}

func runTest(args []string) {
	fs := flag.NewFlagSet("test", flag.ExitOnError)
	baseURL := fs.String("url", "http://127.0.0.1:8080/v1", "Base URL of running proxy")
	model := fs.String("model", "gemini-3.8-flash-high", "Model to test")
	_ = fs.Parse(args)

	fmt.Printf("Sending test completion request to %s/chat/completions (model: %s)...\n", *baseURL, *model)

	reqBody := api.ChatCompletionRequest{
		Model: *model,
		Messages: []api.ChatMessage{
			{Role: "user", Content: "Respond only with: 'GEMINI_PROXY_TEST_SUCCESS'"},
		},
		Stream: false,
	}

	payload, _ := json.Marshal(reqBody)
	resp, err := http.Post(fmt.Sprintf("%s/chat/completions", *baseURL), "application/json", bytes.NewReader(payload))
	if err != nil {
		log.Fatalf("Test request failed: %v. Is gemini-proxy running?", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		log.Fatalf("Server responded with HTTP %d: %s", resp.StatusCode, string(body))
	}

	var parsed api.ChatCompletionResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		log.Fatalf("Failed to parse JSON response: %v\nBody: %s", err, string(body))
	}

	if len(parsed.Choices) > 0 {
		fmt.Printf("\n✓ Received response from %s:\n%s\n", parsed.Model, parsed.Choices[0].Message.Content)
		fmt.Println("✓ GeminiProxy is working perfectly!")
	} else {
		fmt.Println("! Warning: Choices array is empty.")
	}
}
