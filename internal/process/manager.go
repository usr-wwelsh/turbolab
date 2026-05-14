package process

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/usr-wwelsh/turbolab/internal/hf"
	"github.com/usr-wwelsh/turbolab/internal/monitor"
)

type Manager struct {
	mu             sync.Mutex
	cmd            *exec.Cmd
	cancel         context.CancelFunc
	done           chan struct{}
	bin            string
	llamaBin       string
	logOut         io.Writer
	port           int
	model          string // resolved local path (passed to backend)
	requestedModel string // original model ID as requested by caller
	bits           int
	threads        int
	ctxSize        int
	cpuOnly        bool
	noQuant        bool
	running        bool
	loading        bool // true while downloading/starting, false once ready
	intentional    bool // true when stop is deliberate (not a crash)
	crashCount     int  // consecutive fast crashes (< 5s uptime)
}

func New(bin, llamaBin string, logOut io.Writer, port, bits, threads, ctxSize int, cpuOnly, noQuant bool) *Manager {
	if logOut == nil {
		logOut = os.Stdout
	}
	if threads <= 0 {
		threads = physicalCores()
	}
	if ctxSize <= 0 {
		ctxSize = 4096
	}
	return &Manager{bin: bin, llamaBin: llamaBin, logOut: logOut, port: port, bits: bits, threads: threads, ctxSize: ctxSize, cpuOnly: cpuOnly, noQuant: noQuant}
}

func threadStr(n int) string {
	return fmt.Sprintf("%d", n)
}

// physicalCores returns the number of physical CPU cores on Linux,
// falling back to runtime.NumCPU() (logical cores) on other platforms
// or if /proc parsing fails. llama.cpp performs best with physical cores.
func physicalCores() int {
	if runtime.GOOS == "linux" {
		data, err := os.ReadFile("/proc/cpuinfo")
		if err == nil {
			seen := map[string]struct{}{}
			var phys, coreID string
			for _, line := range strings.Split(string(data), "\n") {
				k, v, ok := strings.Cut(line, ":")
				if !ok {
					if line == "" && phys != "" && coreID != "" {
						seen[phys+"/"+coreID] = struct{}{}
						phys, coreID = "", ""
					}
					continue
				}
				k = strings.TrimSpace(k)
				v = strings.TrimSpace(v)
				switch k {
				case "physical id":
					phys = v
				case "core id":
					coreID = v
				}
			}
			if n := len(seen); n > 0 {
				return n
			}
		}
	}
	return runtime.NumCPU()
}

func IsGGUF(modelID string) bool {
	return strings.HasSuffix(strings.ToLower(modelID), ".gguf")
}

func (m *Manager) Start(modelID string) error {
	m.mu.Lock()

	if m.running {
		m.intentional = true
		m.stop()
		m.mu.Unlock()
		// wait for port to be free before starting new process
		if err := m.waitPortFree(); err != nil {
			return err
		}
		m.mu.Lock()
	}

	// Track the original model ID before any resolution so Model() always
	// returns what the caller requested, not the resolved local path.
	m.requestedModel = modelID

	// Resolve locally cached GGUFs (placed in ~/.turbolab/gguf/) to their file path.
	if !IsGGUF(modelID) && !hf.IsGGUFRepo(modelID) {
		if p := hf.FindCachedGGUF(modelID); p != "" {
			modelID = p
		}
	}

	// Resolve HuggingFace GGUF repos to a local file before starting.
	// Release the mutex during the download so status checks don't block.
	if hf.IsGGUFRepo(modelID) {
		m.loading = true
		m.model = modelID
		m.mu.Unlock()

		dlLog := io.MultiWriter(os.Stdout, m.logOut)
		localPath, err := resolveGGUFRepo(modelID, dlLog)
		if err != nil {
			fmt.Fprintf(dlLog, "error: GGUF resolve failed: %v\n", err)
			m.mu.Lock()
			m.loading = false
			m.mu.Unlock()
			return fmt.Errorf("GGUF repo resolve failed: %w", err)
		}
		modelID = localPath
		m.mu.Lock()
	}

	ctx, cancel := context.WithCancel(context.Background())

	var bin string
	var args []string
	var logPrefix string

	if IsGGUF(modelID) {
		if m.llamaBin == "" {
			cancel()
			fmt.Fprintf(io.MultiWriter(os.Stdout, m.logOut), "error: llama-server not found — install llama.cpp and ensure llama-server is in PATH\n")
			m.loading = false
			m.mu.Unlock()
			return fmt.Errorf("llama-server not found — install llama.cpp and ensure llama-server is in PATH")
		}
		bin = m.llamaBin
		logPrefix = "[llama-server] "
		args = []string{
			"--model", modelID,
			"--port", fmt.Sprintf("%d", m.port),
			"--threads", threadStr(m.threads),
			"--threads-batch", threadStr(m.threads),
			"--ctx-size", fmt.Sprintf("%d", m.ctxSize),
			"--batch-size", "2048",
			"--ubatch-size", "512",
			"--cache-reuse", "256",
			"--flash-attn", "auto",
			"--embeddings",
		}
		if mmproj := hf.FindMMProjInDir(filepath.Dir(modelID)); mmproj != "" {
			fmt.Fprintf(io.MultiWriter(os.Stdout, m.logOut), "Vision projector found: %s\n", filepath.Base(mmproj))
			args = append(args, "--mmproj", mmproj)
		}
		if m.cpuOnly {
			args = append(args, "--n-gpu-layers", "0")
		} else {
			args = append(args, "--n-gpu-layers", "99")
		}
	} else {
		bin = m.bin
		logPrefix = "[turboquant] "
		args = []string{"--model", modelID, "--port", fmt.Sprintf("%d", m.port)}
		if m.noQuant {
			args = append(args, "--quantize", "none")
		} else {
			args = append(args, "--bits", fmt.Sprintf("%d", m.bits))
		}
	}

	done := make(chan struct{})
	cmd := exec.CommandContext(ctx, bin, args...)
	threadEnv := []string{
		fmt.Sprintf("OMP_NUM_THREADS=%d", m.threads),
		fmt.Sprintf("MKL_NUM_THREADS=%d", m.threads),
		fmt.Sprintf("OPENBLAS_NUM_THREADS=%d", m.threads),
		fmt.Sprintf("NUMEXPR_NUM_THREADS=%d", m.threads),
		"TOKENIZERS_PARALLELISM=false",
	}
	if IsGGUF(modelID) {
		// Ensure shared libs installed alongside llama-server are found
		if home, err := os.UserHomeDir(); err == nil {
			libDir := filepath.Join(home, ".local", "lib", "llama-cpp")
			existing := os.Getenv("LD_LIBRARY_PATH")
			if existing != "" {
				libDir = libDir + ":" + existing
			}
			threadEnv = append(threadEnv, "LD_LIBRARY_PATH="+libDir)
		}
	}
	if m.cpuOnly && !IsGGUF(modelID) {
		cmd.Env = append(os.Environ(), append(threadEnv, "CUDA_VISIBLE_DEVICES=")...)
	} else {
		cmd.Env = append(os.Environ(), threadEnv...)
	}
	lw := logWriter{prefix: logPrefix, w: io.MultiWriter(os.Stdout, m.logOut)}
	cmd.Stdout = lw
	cmd.Stderr = lw

	if err := cmd.Start(); err != nil {
		cancel()
		m.mu.Unlock()
		return fmt.Errorf("failed to start %s: %w", logPrefix[:len(logPrefix)-2], err)
	}

	m.cmd = cmd
	m.cancel = cancel
	m.done = done
	m.model = modelID
	m.running = true
	m.loading = true
	m.intentional = false
	m.crashCount = 0
	m.mu.Unlock()

	startedAt := time.Now()
	go func() {
		cmd.Wait()
		close(done)
		m.mu.Lock()
		crashed := m.running && !m.intentional
		model := m.model
		requested := m.requestedModel
		uptime := time.Since(startedAt)
		m.running = false
		if crashed && uptime < 5*time.Second {
			m.crashCount++
		} else if !crashed {
			m.crashCount = 0
		}
		crashCount := m.crashCount
		m.mu.Unlock()
		m.mu.Lock()
		m.loading = false
		m.mu.Unlock()
		if crashed {
			prefix := "[turboquant]"
			if IsGGUF(model) {
				prefix = "[llama-server]"
			}
			lw := io.MultiWriter(os.Stdout, m.logOut)
			if crashCount >= 3 {
				fmt.Fprintf(lw, "%s process failed to start (crashed %d times immediately) — check logs above\n", prefix, crashCount)
				return
			}
			fmt.Fprintf(lw, "%s process crashed, restarting in 2s...\n", prefix)
			time.Sleep(2 * time.Second)
			restartID := requested
			if restartID == "" {
				restartID = model
			}
			m.Start(restartID)
		}
	}()

	return m.waitReady()
}

func (m *Manager) Stop() error {
	m.mu.Lock()
	m.intentional = true
	done := m.done
	m.stop()
	m.mu.Unlock()
	if done != nil {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
		}
	}
	return nil
}

func (m *Manager) stop() error {
	if !m.running || m.cancel == nil {
		return nil
	}
	m.cancel()
	m.running = false
	return nil
}

// waitPortFree blocks until the inference port is no longer in use.
func (m *Manager) waitPortFree() error {
	addr := fmt.Sprintf("localhost:%d", m.port)
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err != nil {
			return nil // port is free
		}
		conn.Close()
		time.Sleep(300 * time.Millisecond)
	}
	return fmt.Errorf("port %d still in use after 15s", m.port)
}

func (m *Manager) Running() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.running
}

func (m *Manager) Loading() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.loading
}

func (m *Manager) Model() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.requestedModel != "" {
		return m.requestedModel
	}
	return m.model
}

var healthClient = &http.Client{Timeout: 2 * time.Second}

func (m *Manager) waitReady() error {
	healthURL := fmt.Sprintf("http://localhost:%d/health", m.port)
	deadline := time.Now().Add(30 * time.Minute)
	for time.Now().Before(deadline) {
		m.mu.Lock()
		running := m.running
		exited := m.cmd.ProcessState != nil && m.cmd.ProcessState.Exited()
		code := 0
		if exited {
			code = m.cmd.ProcessState.ExitCode()
		}
		m.mu.Unlock()
		if !running || exited {
			return fmt.Errorf("server exited (code %d) — check logs above", code)
		}
		resp, err := healthClient.Get(healthURL)
		if err == nil && resp.StatusCode == 200 {
			resp.Body.Close()
			m.mu.Lock()
			m.loading = false
			m.mu.Unlock()
			return nil
		}
		if resp != nil {
			resp.Body.Close()
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("server did not become ready within 30 minutes")
}

// resolveGGUFRepo fetches the file list from a HF GGUF repo, picks the best quant
// for available RAM, downloads it if needed, and returns the local file path.
func resolveGGUFRepo(modelID string, logOut io.Writer) (string, error) {
	fmt.Fprintf(logOut, "Resolving GGUF repo: %s\n", modelID)

	// Check local cache before hitting the HF API.
	if cached := hf.FindCachedGGUF(modelID); cached != "" {
		fmt.Fprintf(logOut, "Using cached GGUF: %s\n", cached)
		ensureMMProj(modelID, filepath.Dir(cached), logOut)
		return cached, nil
	}

	info, err := hf.Info(modelID)
	if err != nil {
		return "", err
	}

	stats, _ := monitor.Get()
	var availGB float64
	if stats != nil {
		availGB = stats.AvailableGB()
	}

	filename, err := hf.SelectGGUF(info.Siblings, availGB)
	if err != nil {
		return "", err
	}
	fmt.Fprintf(logOut, "Selected quant: %s (%.1f GB RAM available)\n", filename, availGB)

	localPath, err := hf.DownloadGGUF(modelID, filename, logOut)
	if err != nil {
		return "", err
	}

	if mmproj := hf.SelectMMProj(info.Siblings); mmproj != "" {
		if hf.FindMMProjInDir(filepath.Dir(localPath)) == "" {
			fmt.Fprintf(logOut, "Downloading vision projector: %s\n", mmproj)
			if _, err := hf.DownloadGGUF(modelID, mmproj, logOut); err != nil {
				fmt.Fprintf(logOut, "warning: mmproj download failed: %v (model will load without vision)\n", err)
			}
		}
	}

	return localPath, nil
}

// ensureMMProj fetches the multimodal projector for a cached model if the repo
// ships one and it isn't already on disk. Non-fatal — failure leaves the model
// loadable as text-only.
func ensureMMProj(modelID, modelDir string, logOut io.Writer) {
	if hf.FindMMProjInDir(modelDir) != "" {
		return
	}
	info, err := hf.Info(modelID)
	if err != nil {
		return
	}
	mmproj := hf.SelectMMProj(info.Siblings)
	if mmproj == "" {
		return
	}
	fmt.Fprintf(logOut, "Downloading missing vision projector: %s\n", mmproj)
	if _, err := hf.DownloadGGUF(modelID, mmproj, logOut); err != nil {
		fmt.Fprintf(logOut, "warning: mmproj download failed: %v (model will load without vision)\n", err)
	}
}

type logWriter struct {
	prefix string
	w      io.Writer
}

func (l logWriter) Write(p []byte) (int, error) {
	fmt.Fprintf(l.w, "%s%s", l.prefix, p)
	return len(p), nil
}
