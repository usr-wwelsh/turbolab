package hf

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type localConfig struct {
	Architectures []string `json:"architectures"`
	ModelType     string   `json:"model_type"`
}

func readLocalConfig(cacheDir, modelID string) (*localConfig, error) {
	dirName := "models--" + strings.ReplaceAll(modelID, "/", "--")
	snapshotsDir := filepath.Join(cacheDir, dirName, "snapshots")

	entries, err := os.ReadDir(snapshotsDir)
	if err != nil || len(entries) == 0 {
		return nil, err
	}

	configPath := filepath.Join(snapshotsDir, entries[0].Name(), "config.json")
	f, err := os.Open(configPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var cfg localConfig
	if err := json.NewDecoder(f).Decode(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

var incompatibleModelTypes = map[string]bool{
	"mamba": true, "rwkv": true, "retnet": true, "hgrn": true,
	"granitemoehybrid": true,
}

func compatFromConfig(cfg *localConfig) (bool, string) {
	if cfg == nil {
		return false, "no config"
	}
	if incompatibleModelTypes[strings.ToLower(cfg.ModelType)] {
		return false, cfg.ModelType
	}
	if len(cfg.Architectures) == 0 {
		return false, "unknown arch"
	}
	arch := cfg.Architectures[0]
	if strings.HasSuffix(arch, "ForCausalLM") {
		return true, arch
	}
	return false, arch
}

func LocalModels() ([]Model, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	cacheDir := filepath.Join(home, ".cache", "huggingface", "hub")
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []Model{}, nil
		}
		return nil, err
	}

	var models []Model
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), "models--") {
			continue
		}
		parts := strings.SplitN(strings.TrimPrefix(e.Name(), "models--"), "--", 2)
		if len(parts) != 2 {
			continue
		}
		id := parts[0] + "/" + parts[1]
		cfg, _ := readLocalConfig(cacheDir, id)
		ok, reason := compatFromConfig(cfg)
		modelPath := filepath.Join(cacheDir, e.Name())
		size := dirSize(modelPath)
		models = append(models, Model{
			ID:           id,
			Compatible:   &ok,
			CompatReason: reason,
			SizeBytes:    int64(size),
		})
	}

	// Include downloaded GGUF repos from ~/.turbolab/gguf/
	ggufDir, err := GGUFCacheDir()
	if err == nil {
		ggufEntries, _ := os.ReadDir(ggufDir)
		for _, e := range ggufEntries {
			if !e.IsDir() {
				continue
			}
			// dir names are stored as "namespace--reponame"
			id := strings.Replace(e.Name(), "--", "/", 1)
			ok := false
			ggufPath := filepath.Join(ggufDir, e.Name())
			size := dirSize(ggufPath)
			models = append(models, Model{
				ID:           id,
				Compatible:   &ok,
				CompatReason: "gguf",
				SizeBytes:    int64(size),
			})
		}
	}

	return models, nil
}

func DeleteModel(modelID string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	// Try GGUF cache first
	ggufDir, err := GGUFCacheDir()
	if err == nil {
		safe := strings.ReplaceAll(modelID, "/", "--")
		ggufPath := filepath.Join(ggufDir, safe)
		if _, err := os.Stat(ggufPath); err == nil {
			return os.RemoveAll(ggufPath)
		}
	}

	// Try HuggingFace cache
	cacheDir := filepath.Join(home, ".cache", "huggingface", "hub")
	dirName := "models--" + strings.ReplaceAll(modelID, "/", "--")
	hfPath := filepath.Join(cacheDir, dirName)
	if _, err := os.Stat(hfPath); err == nil {
		return os.RemoveAll(hfPath)
	}

	return fmt.Errorf("model not found: %s", modelID)
}

func CacheSize() (uint64, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return 0, err
	}

	var totalSize uint64

	// HuggingFace cache
	hfDir := filepath.Join(home, ".cache", "huggingface", "hub")
	if entries, err := os.ReadDir(hfDir); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				size := dirSize(filepath.Join(hfDir, e.Name()))
				totalSize += size
			}
		}
	}

	// GGUF cache
	ggufDir, err := GGUFCacheDir()
	if err == nil {
		if entries, err := os.ReadDir(ggufDir); err == nil {
			for _, e := range entries {
				if e.IsDir() {
					size := dirSize(filepath.Join(ggufDir, e.Name()))
					totalSize += size
				}
			}
		}
	}

	return totalSize, nil
}

func dirSize(path string) uint64 {
	var size uint64
	filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		size += uint64(info.Size())
		return nil
	})
	return size
}
