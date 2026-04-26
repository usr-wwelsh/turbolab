package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/usr-wwelsh/turbolab/internal/config"
	"github.com/usr-wwelsh/turbolab/internal/events"
	"github.com/usr-wwelsh/turbolab/internal/hf"
	"github.com/usr-wwelsh/turbolab/internal/monitor"
	"github.com/usr-wwelsh/turbolab/internal/process"
	"github.com/usr-wwelsh/turbolab/internal/proxy"
	"github.com/usr-wwelsh/turbolab/internal/usage"
	"github.com/usr-wwelsh/turbolab/web"
)

var (
	servePort     int
	serveBits     int
	serveModel    string
	serveCPU      bool
	serveNoQuant  bool
	serveThreads  int
	serveCtxSize  int
	inferencePort = 8001
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the turbolab server",
	RunE:  runServe,
}

func init() {
	serveCmd.Flags().IntVarP(&servePort, "port", "p", 0, "Port to listen on (default from config)")
	serveCmd.Flags().IntVar(&serveBits, "bits", 0, "KV cache quantization bits (2, 4, or 8; default from config)")
	serveCmd.Flags().StringVarP(&serveModel, "model", "m", "", "HuggingFace model ID to load on startup (default from config)")
	serveCmd.Flags().BoolVar(&serveCPU, "cpu", false, "Force CPU-only mode (overrides config)")
	serveCmd.Flags().BoolVar(&serveNoQuant, "no-quant", false, "Disable KV cache quantization (for incompatible architectures)")
	serveCmd.Flags().IntVar(&serveThreads, "threads", 0, "CPU threads for inference (0 = all cores)")
	serveCmd.Flags().IntVar(&serveCtxSize, "ctx-size", 0, "Context size in tokens (default from config)")
}

func runServe(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not load config: %v\n", err)
		cfg, _ = config.Load() // use defaults
	}

	// CLI flags override config
	if !cmd.Flags().Changed("port") {
		servePort = cfg.Port
	}
	if !cmd.Flags().Changed("bits") {
		serveBits = cfg.Bits
	}
	if !cmd.Flags().Changed("model") {
		serveModel = cfg.Model
	}
	if !cmd.Flags().Changed("cpu") {
		serveCPU = cfg.CPUOnly
	}
	if !cmd.Flags().Changed("threads") {
		serveThreads = cfg.Threads
	}
	if !cmd.Flags().Changed("ctx-size") {
		serveCtxSize = cfg.CtxSize
	}

	fmt.Printf("turbolab %s — http://localhost:%d\n", version, servePort)

	stats, err := monitor.Get()
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not read system stats: %v\n", err)
	} else {
		fmt.Printf("RAM: %.1f GB available / %.1f GB total\n",
			stats.AvailableGB(), float64(stats.TotalRAM)/1024/1024/1024)
	}

	bin, tqErr := turboquantBin()
	llamaBin := llamaServerBin()
	if tqErr != nil && llamaBin == "" {
		return tqErr
	}
	if tqErr != nil {
		fmt.Fprintf(os.Stderr, "warning: turboquant not found (%v) — GGUF-only mode\n", tqErr)
	}

	usageDB, err := usage.Open()
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not open usage db: %v\n", err)
	}

	hub := events.NewHub()
	mgr := process.New(bin, llamaBin, hub, inferencePort, serveBits, serveThreads, serveCtxSize, serveCPU, serveNoQuant)

	if serveModel != "" {
		if process.IsGGUF(serveModel) {
			fmt.Printf("Loading GGUF model: %s\n", serveModel)
			fmt.Println("Note: turboquant not used — running via llama-server")
		} else {
			fmt.Printf("Loading model: %s (%d-bit KV cache)\n", serveModel, serveBits)
		}
		if err := mgr.Start(serveModel); err != nil {
			return err
		}
		fmt.Println("Model ready.")
	} else {
		fmt.Println("No model loaded. Open the WebUI to load one.")
	}

	mux := http.NewServeMux()

	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		stats, _ := monitor.Get()
		cpu, _ := monitor.CPUPercent()
		home, _ := os.UserHomeDir()
		diskUsage, _ := monitor.DiskUsage(home)
		w.Header().Set("Content-Type", "application/json")
		respMap := map[string]any{
			"version": version,
			"model":   mgr.Model(),
			"running": mgr.Running(),
			"loading": mgr.Loading(),
			"cpu_percent": cpu,
			"ram_available_gb": func() float64 {
				if stats != nil {
					return stats.AvailableGB()
				}
				return 0
			}(),
		}
		if diskUsage != nil {
			respMap["disk_used_gb"] = diskUsage.UsedGB
			respMap["disk_available_gb"] = diskUsage.AvailableGB
		}
		json.NewEncoder(w).Encode(respMap)
	})

	mux.Handle("/api/events", hub.Handler())

	mux.HandleFunc("/api/load", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Model string `json:"model"`
			Bits  int    `json:"bits"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if req.Bits == 0 {
			req.Bits = serveBits
		}
		resp := map[string]string{"status": "loading"}
		if process.IsGGUF(req.Model) {
			fmt.Printf("Loading GGUF model: %s (llama-server, no turboquant)\n", req.Model)
			resp["note"] = "GGUF model: turboquant not used, running via llama-server"
		} else {
			fmt.Printf("Loading model: %s (%d-bit)\n", req.Model, req.Bits)
		}
		go func(model string) {
			if err := mgr.Start(model); err != nil {
				fmt.Fprintf(os.Stderr, "model load failed: %v\n", err)
				hub.Write([]byte("error: model load failed: " + err.Error()))
			}
		}(req.Model)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(resp)
	})

	mux.HandleFunc("/api/config", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			cfg, err := config.Load()
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(cfg)

		case http.MethodPost:
			var incoming config.Config
			if err := json.NewDecoder(r.Body).Decode(&incoming); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if incoming.Bits == 0 {
				incoming.Bits = 4
			}
			if incoming.Port == 0 {
				incoming.Port = 7860
			}
			if err := config.Save(incoming); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"status": "saved"})

		default:
			http.Error(w, "GET or POST only", http.StatusMethodNotAllowed)
		}
	})

	mux.HandleFunc("/api/models/local", func(w http.ResponseWriter, r *http.Request) {
		models, err := hf.LocalModels()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if models == nil {
			models = []hf.Model{}
		}
		json.NewEncoder(w).Encode(models)
	})

	mux.HandleFunc("/api/models/search", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		w.Header().Set("Content-Type", "application/json")

		if strings.Contains(q, "/") {
			info, err := hf.Info(q)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			m := hf.Model{
				ID:          info.ID,
				Downloads:   info.Downloads,
				Likes:       info.Likes,
				Tags:        info.Tags,
				Pipeline:    info.Pipeline,
				LibraryName: info.LibraryName,
			}
			json.NewEncoder(w).Encode([]hf.Model{m})
			return
		}

		limit := 20
		if l := r.URL.Query().Get("limit"); l != "" {
			if n, err := strconv.Atoi(l); err == nil {
				limit = n
			}
		}
		models, err := hf.Search(q, limit)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(models)
	})

	mux.HandleFunc("/api/usage", func(w http.ResponseWriter, r *http.Request) {
		if usageDB == nil {
			http.Error(w, "usage tracking unavailable", http.StatusServiceUnavailable)
			return
		}
		days := 30
		if d := r.URL.Query().Get("days"); d != "" {
			if n, err := strconv.Atoi(d); err == nil && n > 0 {
				days = n
			}
		}
		summary, err := usageDB.Summary(days)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(summary)
	})

	mux.HandleFunc("/api/models/delete", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if req.Model == "" {
			http.Error(w, "model field required", http.StatusBadRequest)
			return
		}
		if err := hf.DeleteModel(req.Model); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})
	})

	var recordFn proxy.RecordFunc
	if usageDB != nil {
		recordFn = usageDB.Record
	}
	inferenceProxy := proxy.New(inferencePort, recordFn)

	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			body, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, "failed to read request body", http.StatusBadRequest)
				return
			}
			var payload map[string]any
			if err := json.Unmarshal(body, &payload); err == nil {
				// Switch model if client requested a different one
				if reqModel, _ := payload["model"].(string); reqModel != "" && reqModel != "default" && reqModel != mgr.Model() {
					if err := mgr.Start(reqModel); err != nil {
						http.Error(w, "failed to load model: "+err.Error(), http.StatusServiceUnavailable)
						return
					}
				}
				modified := false
				if _, set := payload["max_tokens"]; !set {
					cfg, _ := config.Load()
					payload["max_tokens"] = cfg.MaxTokens
					modified = true
				}
				if streaming, _ := payload["stream"].(bool); streaming {
					if _, set := payload["stream_options"]; !set {
						payload["stream_options"] = map[string]any{"include_usage": true}
						modified = true
					}
				}
				if modified {
					if b, err := json.Marshal(payload); err == nil {
						body = b
					}
				}
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
			r.ContentLength = int64(len(body))
		}
		inferenceProxy.ServeHTTP(w, r)
	})

	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		locals, _ := hf.LocalModels()
		type openAIModel struct {
			ID      string `json:"id"`
			Object  string `json:"object"`
			Created int64  `json:"created"`
			OwnedBy string `json:"owned_by"`
		}
		var data []openAIModel
		seen := map[string]bool{}
		// currently loaded model first
		if m := mgr.Model(); m != "" {
			data = append(data, openAIModel{ID: m, Object: "model", Created: 0, OwnedBy: "turbolab"})
			seen[m] = true
		}
		for _, m := range locals {
			if !seen[m.ID] {
				data = append(data, openAIModel{ID: m.ID, Object: "model", Created: 0, OwnedBy: "turbolab"})
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": data})
	})

	mux.Handle("/v1/", inferenceProxy)

	mux.Handle("/", web.Handler())

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sig
		fmt.Println("\nShutting down...")
		mgr.Stop()
		if usageDB != nil {
			usageDB.Close()
		}
		os.Exit(0)
	}()

	return http.ListenAndServe(fmt.Sprintf(":%d", servePort), mux)
}
