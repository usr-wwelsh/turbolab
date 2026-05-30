package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/usr-wwelsh/turbolab/internal/config"
	"github.com/usr-wwelsh/turbolab/internal/events"
	"github.com/usr-wwelsh/turbolab/internal/hf"
	"github.com/usr-wwelsh/turbolab/internal/mcp"
	"github.com/usr-wwelsh/turbolab/internal/memory"
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
	idPort        = 8002
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

	memDB, err := memory.Open()
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not open memory db: %v\n", err)
	}

	mcpOn := &atomic.Bool{}
	mcpOn.Store(cfg.MCPEnabled)
	mcpSrv := mcp.New(memDB, mcpOn)

	memInjectOn := &atomic.Bool{}
	memInjectOn.Store(cfg.MemoryInject)

	// tracks which memory IDs have been injected per conversation (keyed by fnv of first user message)
	var convInjectMu sync.Mutex
	convInjected := map[uint64]map[string]bool{}
	// Batch workloads (e.g. summarizing a whole corpus) send a unique first user
	// message per request, so every call mints a new key. Without a cap this map
	// grows once per request forever — unbounded RSS until OOM. Conversations are
	// short-lived; dropping old keys at most re-injects a memory once, harmless.
	const maxTrackedConvs = 1024

	hub := events.NewHub()
	mgr := process.New(bin, llamaBin, hub, inferencePort, serveBits, serveThreads, serveCtxSize, serveCPU, serveNoQuant, false)
	idMgr := process.New(bin, llamaBin, hub, idPort, serveBits, serveThreads, 2048, serveCPU, serveNoQuant, true)

	// Some backends (notably llama.cpp on certain model archs) leak memory across
	// requests; recycle the child once its RSS crosses the configured ceiling so a
	// long batch run can't OOM the host. Off by default.
	if cfg.RecycleRSSMB > 0 {
		limit := uint64(cfg.RecycleRSSMB) << 20
		mgr.SetRecycleRSS(limit)
		idMgr.SetRecycleRSS(limit)
		fmt.Printf("Inference auto-recycle: restart child above %d MB RSS\n", cfg.RecycleRSSMB)
	}

	if memDB != nil {
		memDB.SetEmbedFunc(makeEmbedFunc(idPort, idMgr))
	}

	if cfg.IDModel != "" {
		fmt.Printf("Loading ID model: %s\n", cfg.IDModel)
		if err := idMgr.Start(cfg.IDModel); err != nil {
			fmt.Fprintf(os.Stderr, "warning: ID model failed to load: %v\n", err)
		} else {
			fmt.Println("ID model ready.")
			if memDB != nil {
				memDB.RebuildEmbeddings()
			}
		}
	}

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
			"version":          version,
			"model":            mgr.Model(),
			"running":          mgr.Running(),
			"loading":          mgr.Loading(),
			"cpu_percent":      cpu,
			"mcp_enabled":      mcpOn.Load(),
			"memory_inject":    memInjectOn.Load(),
			"id_model":         idMgr.Model(),
			"id_model_running": idMgr.Running(),
			"id_model_loading": idMgr.Loading(),
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

	mux.HandleFunc("/api/id-model/load", func(w http.ResponseWriter, r *http.Request) {
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
		go func(model string) {
			if model == "" {
				idMgr.Stop()
				return
			}
			if err := idMgr.Start(model); err != nil {
				fmt.Fprintf(os.Stderr, "ID model load failed: %v\n", err)
				hub.Write([]byte("error: ID model load failed: " + err.Error()))
				return
			}
			if memDB != nil {
				memDB.RebuildEmbeddings()
			}
		}(req.Model)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(map[string]string{"status": "loading"})
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
			mcpOn.Store(incoming.MCPEnabled)
			memInjectOn.Store(incoming.MemoryInject)
			if incoming.IDModel != idMgr.Model() {
				go func(model string) {
					if model == "" {
						idMgr.Stop()
						return
					}
					if err := idMgr.Start(model); err != nil {
						fmt.Fprintf(os.Stderr, "ID model load failed: %v\n", err)
						return
					}
					if memDB != nil {
						memDB.RebuildEmbeddings()
					}
				}(incoming.IDModel)
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

	mux.HandleFunc("/mcp", mcpSrv.Handler())

	if memDB != nil {
		mux.HandleFunc("/api/memory/list", func(w http.ResponseWriter, r *http.Request) {
			limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
			offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
			if limit <= 0 {
				limit = 50
			}
			mems, err := memDB.List(limit, offset)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(mems)
		})

		mux.HandleFunc("/api/memory/search", func(w http.ResponseWriter, r *http.Request) {
			q := r.URL.Query().Get("q")
			limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
			if limit <= 0 {
				limit = 20
			}
			mems, err := memDB.Search(q, limit)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(mems)
		})

		mux.HandleFunc("/api/memory/add", func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				http.Error(w, "POST only", http.StatusMethodNotAllowed)
				return
			}
			var req struct {
				Content string   `json:"content"`
				Source  string   `json:"source"`
				Tags    []string `json:"tags"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if strings.TrimSpace(req.Content) == "" {
				http.Error(w, "content required", http.StatusBadRequest)
				return
			}
			m, err := memDB.Add(req.Content, req.Source, req.Tags)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(m)
		})

		mux.HandleFunc("/api/memory/delete", func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				http.Error(w, "POST only", http.StatusMethodNotAllowed)
				return
			}
			var req struct {
				ID string `json:"id"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if err := memDB.Delete(req.ID); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})
		})

		mux.HandleFunc("/api/memory/relate", func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				http.Error(w, "POST only", http.StatusMethodNotAllowed)
				return
			}
			var req struct {
				FromID  string `json:"from_id"`
				ToID    string `json:"to_id"`
				RelType string `json:"rel_type"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if err := memDB.Relate(req.FromID, req.ToID, req.RelType); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"status": "related"})
		})

		mux.HandleFunc("/api/memory/unrelate", func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				http.Error(w, "POST only", http.StatusMethodNotAllowed)
				return
			}
			var req struct {
				FromID  string `json:"from_id"`
				ToID    string `json:"to_id"`
				RelType string `json:"rel_type"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if err := memDB.Unrelate(req.FromID, req.ToID, req.RelType); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"status": "unrelated"})
		})

		mux.HandleFunc("/api/memory/graph", func(w http.ResponseWriter, r *http.Request) {
			data, err := memDB.GraphData()
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(data)
		})

		mux.HandleFunc("/api/memory/semantic-search", func(w http.ResponseWriter, r *http.Request) {
			q := r.URL.Query().Get("q")
			limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
			if limit <= 0 {
				limit = 10
			}
			minScore := float32(0.3)
			results, err := memDB.SemanticSearch(q, limit, minScore)
			if err != nil {
				http.Error(w, err.Error(), http.StatusServiceUnavailable)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(results)
		})

		mux.HandleFunc("/api/memory/stats", func(w http.ResponseWriter, r *http.Request) {
			total, vectorized, err := memDB.Stats()
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]int{"total": total, "vectorized": vectorized})
		})

		mux.HandleFunc("/api/memory/embed-rebuild", func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				http.Error(w, "POST only", http.StatusMethodNotAllowed)
				return
			}
			memDB.RebuildEmbeddings()
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"status": "rebuilding"})
		})

		mux.HandleFunc("/api/memory/convert", func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				http.Error(w, "POST only", http.StatusMethodNotAllowed)
				return
			}
			if err := r.ParseMultipartForm(32 << 20); err != nil {
				http.Error(w, "failed to parse form", http.StatusBadRequest)
				return
			}
			file, header, err := r.FormFile("file")
			if err != nil {
				http.Error(w, "file field required", http.StatusBadRequest)
				return
			}
			defer file.Close()
			data, err := io.ReadAll(file)
			if err != nil {
				http.Error(w, "failed to read file", http.StatusInternalServerError)
				return
			}
			md, err := memory.ConvertToMarkdown(header.Filename, data)
			if err != nil {
				http.Error(w, err.Error(), http.StatusUnprocessableEntity)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{
				"markdown": md,
				"filename": header.Filename,
			})
		})
	}

	var recordFn proxy.RecordFunc
	if usageDB != nil {
		recordFn = usageDB.Record
	}
	inferenceProxy := proxy.New(inferencePort, recordFn)
	embedProxy := proxy.New(idPort, recordFn)
	backendURL := fmt.Sprintf("http://localhost:%d", inferencePort)
	backendClient := &http.Client{Timeout: 10 * time.Minute}

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
				if memInjectOn.Load() && memDB != nil && idMgr.Running() {
					var firstUserMsg, lastUserMsg string
					if msgs, ok := payload["messages"].([]any); ok {
						for _, m := range msgs {
							msg, ok := m.(map[string]any)
							if !ok {
								continue
							}
							if role, _ := msg["role"].(string); role == "user" {
								if content, _ := msg["content"].(string); content != "" {
									if firstUserMsg == "" {
										firstUserMsg = content
									}
									lastUserMsg = content
								}
							}
						}
					}
					if lastUserMsg != "" && len(lastUserMsg) >= 20 {
						h := fnv.New64a()
						h.Write([]byte(firstUserMsg))
						convKey := h.Sum64()

						convInjectMu.Lock()
						seen := convInjected[convKey]
						if seen == nil {
							if len(convInjected) >= maxTrackedConvs {
								convInjected = map[uint64]map[string]bool{}
							}
							seen = map[string]bool{}
							convInjected[convKey] = seen
						}
						convInjectMu.Unlock()

						cfg, _ := config.Load()
						minScore := cfg.MemoryInjectMinScore
						if minScore <= 0 {
							minScore = 0.6
						}
						scored, err := memDB.SemanticSearch(lastUserMsg, 2, minScore)
						if err == nil {
							var lines []string
							convInjectMu.Lock()
							for _, s := range scored {
								if seen[s.ID] {
									continue
								}
								c := s.Content
								if len(c) > 400 {
									c = c[:400] + "…"
								}
								lines = append(lines, "• "+c)
								seen[s.ID] = true
							}
							convInjectMu.Unlock()
							if len(lines) > 0 {
								memCtx := "Relevant context from memory:\n" + strings.Join(lines, "\n")
								if msgs, ok := payload["messages"].([]any); ok {
									if len(msgs) > 0 {
										if first, ok := msgs[0].(map[string]any); ok {
											if role, _ := first["role"].(string); role == "system" {
												if existing, ok := first["content"].(string); ok {
													first["content"] = memCtx + "\n\n" + existing
												} else {
													first["content"] = memCtx
												}
											} else {
												payload["messages"] = append([]any{map[string]any{"role": "system", "content": memCtx}}, msgs...)
											}
										}
									} else {
										payload["messages"] = []any{map[string]any{"role": "system", "content": memCtx}}
									}
								}
								modified = true
							}
						}
					}
				}

				// MCP tool injection + tool-call loop.
				// Only inject when the client hasn't provided its own tools (client handles its own loop).
				clientHasTools := false
				if t, ok := payload["tools"]; ok && t != nil {
					if ts, ok := t.([]any); ok && len(ts) > 0 {
						clientHasTools = true
					}
				}
				if mcpOn.Load() && memDB != nil && !clientHasTools {
					payload["tools"] = mcpSrv.ToolDefs()
					modified = true

					originalStreaming, _ := payload["stream"].(bool)
					payload["stream"] = false

					var finalRespBody []byte
					loopDone := false
					const maxIter = 10
					for i := 0; i < maxIter && !loopDone; i++ {
						subBody, err := json.Marshal(payload)
						if err != nil {
							break
						}
						resp, err := backendClient.Post(backendURL+"/v1/chat/completions", "application/json", bytes.NewReader(subBody))
						if err != nil {
							// backend unavailable — restore stream and fall through to proxy
							payload["stream"] = originalStreaming
							break
						}
						respBody, _ := io.ReadAll(resp.Body)
						resp.Body.Close()

						var respData map[string]any
						if json.Unmarshal(respBody, &respData) != nil {
							finalRespBody = respBody
							loopDone = true
							break
						}
						choices, _ := respData["choices"].([]any)
						if len(choices) == 0 {
							finalRespBody = respBody
							loopDone = true
							break
						}
						choice, _ := choices[0].(map[string]any)
						msg, _ := choice["message"].(map[string]any)
						finishReason, _ := choice["finish_reason"].(string)

						if finishReason != "tool_calls" {
							finalRespBody = respBody
							loopDone = true
							break
						}

						// Append assistant message then execute each tool call
						msgs, _ := payload["messages"].([]any)
						msgs = append(msgs, msg)
						toolCalls, _ := msg["tool_calls"].([]any)
						for _, tc := range toolCalls {
							tcMap, _ := tc.(map[string]any)
							tcID, _ := tcMap["id"].(string)
							fn, _ := tcMap["function"].(map[string]any)
							name, _ := fn["name"].(string)
							argsStr, _ := fn["arguments"].(string)
							result, err := mcpSrv.CallTool(name, json.RawMessage(argsStr))
							content := result
							if err != nil {
								content = "error: " + err.Error()
							}
							msgs = append(msgs, map[string]any{
								"role":         "tool",
								"tool_call_id": tcID,
								"content":      content,
							})
						}
						payload["messages"] = msgs
					}

					if loopDone && finalRespBody != nil {
						if recordFn != nil {
							proxy.RecordFromJSON(finalRespBody, recordFn)
						}
						if originalStreaming {
							writeAsSSE(w, finalRespBody)
						} else {
							w.Header().Set("Content-Type", "application/json")
							w.Write(finalRespBody)
						}
						return
					}
					// loop didn't complete (backend error) — fall through to proxy
					payload["stream"] = originalStreaming
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

	// Embeddings route to the dedicated ID/embedding model (idPort), not the
	// chat model. Must be registered before the "/v1/" catch-all.
	mux.HandleFunc("/v1/embeddings", func(w http.ResponseWriter, r *http.Request) {
		if !idMgr.Running() {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`{"error":{"message":"no embedding model loaded","type":"service_unavailable"}}`))
			return
		}
		embedProxy.ServeHTTP(w, r)
	})

	mux.Handle("/v1/", inferenceProxy)

	mux.Handle("/", web.Handler())

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sig
		fmt.Println("\nShutting down...")
		mgr.Stop()
		idMgr.Stop()
		if usageDB != nil {
			usageDB.Close()
		}
		if memDB != nil {
			memDB.Close()
		}
		os.Exit(0)
	}()

	return http.ListenAndServe(fmt.Sprintf(":%d", servePort), mux)
}

func writeAsSSE(w http.ResponseWriter, jsonBody []byte) {
	var resp struct {
		ID      string `json:"id"`
		Created int64  `json:"created"`
		Model   string `json:"model"`
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage *struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	flush := func() {
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}
	if json.Unmarshal(jsonBody, &resp) != nil || len(resp.Choices) == 0 {
		fmt.Fprintf(w, "data: [DONE]\n\n")
		flush()
		return
	}
	contentChunk := map[string]any{
		"id":      resp.ID,
		"object":  "chat.completion.chunk",
		"created": resp.Created,
		"model":   resp.Model,
		"choices": []map[string]any{{
			"index":         0,
			"delta":         map[string]any{"role": "assistant", "content": resp.Choices[0].Message.Content},
			"finish_reason": nil,
		}},
	}
	if b, err := json.Marshal(contentChunk); err == nil {
		fmt.Fprintf(w, "data: %s\n\n", b)
	}
	finalChunk := map[string]any{
		"id":      resp.ID,
		"object":  "chat.completion.chunk",
		"created": resp.Created,
		"model":   resp.Model,
		"choices": []map[string]any{{
			"index":         0,
			"delta":         map[string]any{},
			"finish_reason": resp.Choices[0].FinishReason,
		}},
	}
	if resp.Usage != nil {
		finalChunk["usage"] = resp.Usage
	}
	if b, err := json.Marshal(finalChunk); err == nil {
		fmt.Fprintf(w, "data: %s\n\n", b)
	}
	fmt.Fprintf(w, "data: [DONE]\n\n")
	flush()
}

func makeEmbedFunc(port int, mgr *process.Manager) memory.EmbedFunc {
	client := &http.Client{Timeout: 30 * time.Second}
	return func(text string) ([]float32, error) {
		if !mgr.Running() {
			return nil, fmt.Errorf("no model loaded")
		}
		body, _ := json.Marshal(map[string]any{"input": text, "model": "default"})
		resp, err := client.Post(
			fmt.Sprintf("http://localhost:%d/v1/embeddings", port),
			"application/json",
			bytes.NewReader(body),
		)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("embedding endpoint returned %d", resp.StatusCode)
		}
		var result struct {
			Data []struct {
				Embedding []float32 `json:"embedding"`
			} `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return nil, err
		}
		if len(result.Data) == 0 || len(result.Data[0].Embedding) == 0 {
			return nil, fmt.Errorf("empty embedding response")
		}
		return result.Data[0].Embedding, nil
	}
}
