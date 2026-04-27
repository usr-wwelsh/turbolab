package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

const DefaultModel = "bigatuna/Qwen3-0.6B-Sushi-Coder"

type Config struct {
	Model      string `json:"model"`
	CPUOnly    bool   `json:"cpu_only"`
	Bits       int    `json:"bits"`
	Port       int    `json:"port"`
	MaxTokens  int    `json:"max_tokens"`
	Threads    int    `json:"threads"`
	CtxSize    int    `json:"ctx_size"`
	MCPEnabled            bool    `json:"mcp_enabled"`
	MemoryInject          bool    `json:"memory_inject"`
	MemoryInjectMinScore  float32 `json:"memory_inject_min_score"`
	IDModel               string  `json:"id_model"`
}

const DefaultIDModel = "ggml-org/e5-small-v2-Q8_0-GGUF"

func defaults() Config {
	return Config{
		Model:     DefaultModel,
		CPUOnly:              true,
		Bits:                 4,
		Port:                 7860,
		MaxTokens:            2048,
		Threads:              0,
		CtxSize:              2048,
		IDModel:              DefaultIDModel,
		MemoryInjectMinScore: 0.75,
	}
}

func path() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".turbolab", "config.json"), nil
}

func Load() (Config, error) {
	cfg := defaults()
	p, err := path()
	if err != nil {
		return cfg, err
	}
	f, err := os.Open(p)
	if os.IsNotExist(err) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	defer f.Close()
	if err := json.NewDecoder(f).Decode(&cfg); err != nil {
		return cfg, err
	}
	// fill in zero values with defaults
	if cfg.Bits == 0 {
		cfg.Bits = 4
	}
	if cfg.Port == 0 {
		cfg.Port = 7860
	}
	if cfg.Model == "" {
		cfg.Model = DefaultModel
	}
	if cfg.MaxTokens == 0 {
		cfg.MaxTokens = 2048
	}
	if cfg.CtxSize == 0 {
		cfg.CtxSize = 2048
	}
	if cfg.IDModel == "" {
		cfg.IDModel = DefaultIDModel
	}
	if cfg.MemoryInjectMinScore <= 0 {
		cfg.MemoryInjectMinScore = 0.75
	}
	return cfg, nil
}

func Save(cfg Config) error {
	p, err := path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		return err
	}
	f, err := os.Create(p)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(cfg)
}
