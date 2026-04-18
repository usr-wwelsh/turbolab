package hf

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const apiBase = "https://huggingface.co/api"

var httpClient = &http.Client{Timeout: 15 * time.Second}

// incompatibleTags identifies model types that don't work with turboquant's KV cache.
var incompatibleTags = []string{
	"gguf", "ggml", "rwkv", "mamba", "ssm",
	"granitemoehybrid", "hybrid", "retnet", "hgrn",
}

type Model struct {
	ID           string   `json:"id"`
	Downloads    int      `json:"downloads"`
	Likes        int      `json:"likes"`
	Tags         []string `json:"tags"`
	Pipeline     string   `json:"pipeline_tag"`
	LibraryName  string   `json:"library_name"`
	// set for local models, null for search results (frontend uses tag heuristic)
	Compatible   *bool    `json:"compatible,omitempty"`
	CompatReason string   `json:"compat_reason,omitempty"`
	SizeBytes    int64    `json:"size_bytes,omitempty"`
}

// SizeLabel parses a human-readable size from tags (e.g. "7B", "1.5B").
func (m *Model) SizeLabel() string {
	for _, tag := range m.Tags {
		t := strings.ToLower(tag)
		// match patterns like "7b", "1.5b", "70b"
		if len(t) >= 2 && t[len(t)-1] == 'b' {
			prefix := t[:len(t)-1]
			if len(prefix) > 0 && (prefix[0] >= '0' && prefix[0] <= '9') {
				return strings.ToUpper(tag)
			}
		}
	}
	return "?"
}

func (m *Model) CheckCompat() (bool, string) {
	tagSet := make(map[string]bool, len(m.Tags))
	for _, t := range m.Tags {
		tagSet[strings.ToLower(t)] = true
	}
	for _, bad := range incompatibleTags {
		if tagSet[bad] {
			return false, bad
		}
	}
	// Must use transformers library and have safetensors
	if m.LibraryName != "" && m.LibraryName != "transformers" {
		return false, m.LibraryName
	}
	if tagSet["safetensors"] {
		return true, "✓"
	}
	return false, "?"
}

type ModelInfo struct {
	ID          string      `json:"id"`
	Downloads   int         `json:"downloads"`
	Likes       int         `json:"likes"`
	Tags        []string    `json:"tags"`
	Pipeline    string      `json:"pipeline_tag"`
	Siblings    []ModelFile `json:"siblings"`
	LibraryName string      `json:"library_name"`
}

type ModelFile struct {
	Filename string `json:"rfilename"`
	Size     int64  `json:"size"`
}

func Search(query string, limit int) ([]Model, error) {
	u := fmt.Sprintf("%s/models?search=%s&filter=text-generation&sort=downloads&direction=-1&limit=%d&full=true",
		apiBase, url.QueryEscape(query), limit)

	resp, err := httpClient.Get(u)
	if err != nil {
		return nil, fmt.Errorf("hf search: %w", err)
	}
	defer resp.Body.Close()

	var models []Model
	if err := json.NewDecoder(resp.Body).Decode(&models); err != nil {
		return nil, fmt.Errorf("hf decode: %w", err)
	}
	return models, nil
}

func Info(modelID string) (*ModelInfo, error) {
	u := fmt.Sprintf("%s/models/%s", apiBase, modelID)

	resp, err := httpClient.Get(u)
	if err != nil {
		return nil, fmt.Errorf("hf info: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		return nil, fmt.Errorf("model not found: %s", modelID)
	}

	var info ModelInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("hf decode: %w", err)
	}
	return &info, nil
}
