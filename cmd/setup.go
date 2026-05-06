package cmd

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
)

var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Install turboquant and Python dependencies",
	RunE:  runSetup,
}

func runSetup(cmd *cobra.Command, args []string) error {
	venvDir, err := venvPath()
	if err != nil {
		return err
	}

	pip := filepath.Join(venvDir, "bin", "pip")
	if _, err := os.Stat(pip); err != nil {
		fmt.Println("Creating Python venv...")
		python := pythonBin()
		fmt.Printf("Using Python: %s\n", python)
		if err := run(python, "-m", "venv", venvDir); err != nil {
			return fmt.Errorf("venv creation failed: %w", err)
		}
	} else {
		fmt.Println("Venv already exists, updating...")
	}
	fmt.Println("Installing turboquant + dependencies...")
	if err := run(pip, "install", "--upgrade", "--retries", "5",
		"turboquant",
		"accelerate",
		"sentencepiece",
		"protobuf",
		"tiktoken",
	); err != nil {
		return fmt.Errorf("pip install failed: %w", err)
	}

	if llamaServerBin() != "" {
		fmt.Println("llama-server found — GGUF models supported.")
	} else {
		fmt.Println("llama-server not found.")
		if err := installLlamaServer(); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not install llama-server: %v\n", err)
			fmt.Println("Install manually: https://github.com/ggerganov/llama.cpp/releases")
		}
	}

	if err := promptSystemd(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: systemd setup failed: %v\n", err)
	}

	fmt.Println("Setup complete. Run: turbolab serve --model <model-id>")
	return nil
}

func installLlamaServer() error {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		return fmt.Errorf("auto-install only supported on linux/amd64")
	}

	fmt.Println("Fetching latest llama.cpp release...")
	resp, err := http.Get("https://api.github.com/repos/ggerganov/llama.cpp/releases/latest")
	if err != nil {
		return fmt.Errorf("GitHub API: %w", err)
	}
	defer resp.Body.Close()

	var release struct {
		Assets []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return fmt.Errorf("parse release: %w", err)
	}

	var assetURL, assetName string
	for _, a := range release.Assets {
		if strings.Contains(a.Name, "ubuntu-x64") && strings.HasSuffix(a.Name, ".tar.gz") {
			assetURL = a.BrowserDownloadURL
			assetName = a.Name
			break
		}
	}
	if assetURL == "" {
		return fmt.Errorf("no ubuntu-x64 asset found in latest release")
	}

	fmt.Printf("Downloading %s...\n", assetName)
	r, err := http.Get(assetURL)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer r.Body.Close()

	gz, err := gzip.NewReader(r.Body)
	if err != nil {
		return fmt.Errorf("gzip: %w", err)
	}
	defer gz.Close()

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("homedir: %w", err)
	}
	installDir := filepath.Join(home, ".local", "lib", "llama-cpp")
	if err := os.MkdirAll(installDir, 0755); err != nil {
		return fmt.Errorf("mkdir %s: %w", installDir, err)
	}

	tr := tar.NewReader(gz)
	installed := false
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("tar: %w", err)
		}
		base := filepath.Base(hdr.Name)
		isBin := base == "llama-server"
		isSO := strings.HasSuffix(base, ".so") || strings.Contains(base, ".so.")
		if !isBin && !isSO {
			continue
		}

		dest := filepath.Join(installDir, base)

		if hdr.Typeflag == tar.TypeSymlink {
			os.Remove(dest)
			if err := os.Symlink(hdr.Linkname, dest); err != nil {
				return fmt.Errorf("symlink %s: %w", dest, err)
			}
			fmt.Printf("Symlinked %s → %s\n", base, hdr.Linkname)
			continue
		}

		out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
		if err != nil {
			return fmt.Errorf("install to %s: %w", dest, err)
		}
		_, err = io.Copy(out, tr)
		out.Close()
		if err != nil {
			return err
		}
		fmt.Printf("Installed %s\n", base)
		if isBin {
			installed = true
		}
	}
	if !installed {
		return fmt.Errorf("llama-server binary not found in tar.gz")
	}

	// Symlink binary into ~/.local/bin (already in PATH for most users)
	binDir := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		return fmt.Errorf("mkdir %s: %w", binDir, err)
	}
	binLink := filepath.Join(binDir, "llama-server")
	os.Remove(binLink)
	if err := os.Symlink(filepath.Join(installDir, "llama-server"), binLink); err != nil {
		return fmt.Errorf("symlink binary: %w", err)
	}
	fmt.Printf("Symlinked llama-server → %s\n", binLink)
	return nil
}

func promptSystemd() error {
	// only offer on systemd systems
	if _, err := exec.LookPath("systemctl"); err != nil {
		return nil
	}

	fmt.Print("Install turbolab as a systemd service? [y/N] ")
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Scan()
	if strings.ToLower(strings.TrimSpace(scanner.Text())) != "y" {
		return nil
	}

	const installPath = "/usr/local/bin/turbolab"
	if _, err := os.Stat(installPath); os.IsNotExist(err) {
		self, err := os.Executable()
		if err != nil {
			return fmt.Errorf("could not determine binary path: %w", err)
		}
		fmt.Printf("Copying binary to %s...\n", installPath)
		if err := copyFile(self, installPath, 0755); err != nil {
			return fmt.Errorf("couldn't install to %s (try sudo): %w", installPath, err)
		}
	} else {
		fmt.Printf("Binary already present at %s\n", installPath)
	}

	unit := `[Unit]
Description=turbolab AI model server
After=network.target

[Service]
ExecStart=/usr/local/bin/turbolab serve
Restart=on-failure
RestartSec=5
StartLimitIntervalSec=60
StartLimitBurst=3
KillMode=mixed
KillSignal=SIGTERM
TimeoutStopSec=30

[Install]
WantedBy=multi-user.target
`

	unitPath := "/etc/systemd/system/turbolab.service"
	if err := os.WriteFile(unitPath, []byte(unit), 0644); err != nil {
		return fmt.Errorf("couldn't write unit file (try sudo): %w", err)
	}

	if err := run("systemctl", "daemon-reload"); err != nil {
		return err
	}
	if err := run("systemctl", "enable", "turbolab"); err != nil {
		return err
	}

	fmt.Println("Service installed. Start with: sudo systemctl start turbolab")
	return nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	return out.Close()
}

func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func pythonBin() string {
	home, _ := os.UserHomeDir()
	// prefer pyenv Python 3.12 (has prebuilt numpy wheels, avoids compile hell)
	pyenvBase := filepath.Join(home, ".pyenv", "versions")
	entries, _ := os.ReadDir(pyenvBase)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "3.12") {
			bin := filepath.Join(pyenvBase, e.Name(), "bin", "python3")
			if _, err := os.Stat(bin); err == nil {
				return bin
			}
		}
	}
	return "python3"
}

func venvPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".turbolab", "venv"), nil
}

func llamaServerBin() string {
	path, err := exec.LookPath("llama-server")
	if err != nil {
		return ""
	}
	// Verify shared libs load — exit 127 means missing .so files
	if err := exec.Command(path, "--version").Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 127 {
			return ""
		}
	}
	return path
}

func turboquantBin() (string, error) {
	venvDir, err := venvPath()
	if err != nil {
		return "", err
	}
	bin := filepath.Join(venvDir, "bin", "turboquant-server")
	if _, err := os.Stat(bin); err == nil {
		return bin, nil
	}
	// fallback to PATH
	if path, err := exec.LookPath("turboquant-server"); err == nil {
		return path, nil
	}
	return "", fmt.Errorf("turboquant-server not found — run: turbolab setup")
}
