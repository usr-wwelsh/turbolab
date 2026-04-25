package hf

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// quantRank maps quant suffixes to a preference score (lower = better for CPU speed/quality balance).
// quantRank maps quant suffixes to a preference score (lower = better).
// Q4_0 ranks first — fastest AVX2 dot-product paths in llama.cpp, best decode
// throughput on CPU-only. Q4_K_M is second for quality when speed is acceptable.
var quantRank = map[string]int{
	"Q4_0":     1,
	"Q4_K_M":   2,
	"Q4_K_S":   3,
	"Q5_K_M":   4,
	"Q5_K_S":   5,
	"Q3_K_M":   6,
	"Q3_K_L":   7,
	"Q4_1":     8,
	"Q6_K":     9,
	"Q8_0":     10,
	"Q2_K":     11,
	"Q2_K_S":   12,
	"IQ4_XS":   13,
	"IQ3_M":    14,
	"IQ2_M":    15,
}

// IsGGUFRepo returns true if the model ID looks like a HuggingFace GGUF repository
// (contains -gguf in the name, but is not a local .gguf file path).
func IsGGUFRepo(modelID string) bool {
	upper := strings.ToUpper(modelID)
	return strings.Contains(modelID, "/") &&
		!strings.HasSuffix(upper, ".GGUF") &&
		strings.Contains(upper, "-GGUF")
}

// SelectGGUF picks the best GGUF filename from a list of repo files given available RAM in GB.
// Prefers Q4_K_M for CPU (best speed/quality tradeoff). Skips files too large to fit.
func SelectGGUF(files []ModelFile, availableGB float64) (string, error) {
	type candidate struct {
		filename string
		rank     int
		sizeGB   float64
	}

	var candidates []candidate
	for _, f := range files {
		name := f.Filename
		if !strings.HasSuffix(strings.ToLower(name), ".gguf") {
			continue
		}
		// Skip split shards (e.g. model-00001-of-00003.gguf)
		if strings.Contains(name, "-of-") {
			continue
		}
		sizeGB := float64(f.Size) / 1024 / 1024 / 1024
		// Keep 1GB headroom
		if availableGB > 0 && sizeGB > availableGB-1.0 {
			continue
		}
		rank := rankGGUF(name)
		candidates = append(candidates, candidate{name, rank, sizeGB})
	}

	if len(candidates) == 0 {
		// If RAM filter eliminated everything, fall back to smallest file
		var smallest *ModelFile
		for i := range files {
			f := &files[i]
			if !strings.HasSuffix(strings.ToLower(f.Filename), ".gguf") {
				continue
			}
			if strings.Contains(f.Filename, "-of-") {
				continue
			}
			if smallest == nil || f.Size < smallest.Size {
				smallest = f
			}
		}
		if smallest != nil {
			return smallest.Filename, nil
		}
		return "", fmt.Errorf("no GGUF files found in repo")
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].rank != candidates[j].rank {
			return candidates[i].rank < candidates[j].rank
		}
		return candidates[i].sizeGB < candidates[j].sizeGB
	})

	return candidates[0].filename, nil
}

func rankGGUF(filename string) int {
	upper := strings.ToUpper(filename)
	for suffix, rank := range quantRank {
		if strings.Contains(upper, suffix) {
			return rank
		}
	}
	return 99
}

// GGUFCacheDir returns the directory where downloaded GGUFs are stored.
func GGUFCacheDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".turbolab", "gguf")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	return dir, nil
}

// LocalGGUFPath returns the local path for a downloaded GGUF file, or "" if not cached.
func LocalGGUFPath(modelID, filename string) string {
	dir, err := GGUFCacheDir()
	if err != nil {
		return ""
	}
	safe := strings.ReplaceAll(modelID, "/", "--")
	p := filepath.Join(dir, safe, filename)
	if _, err := os.Stat(p); err != nil {
		return ""
	}
	return p
}

// DownloadGGUF downloads a GGUF file from HuggingFace to local cache.
// Progress is written to w. Returns the local file path.
func DownloadGGUF(modelID, filename string, w io.Writer) (string, error) {
	dir, err := GGUFCacheDir()
	if err != nil {
		return "", err
	}
	safe := strings.ReplaceAll(modelID, "/", "--")
	destDir := filepath.Join(dir, safe)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return "", err
	}
	dest := filepath.Join(destDir, filename)

	// Already cached
	if _, err := os.Stat(dest); err == nil {
		fmt.Fprintf(w, "GGUF already cached: %s\n", dest)
		return dest, nil
	}

	dlURL := fmt.Sprintf("https://huggingface.co/%s/resolve/main/%s", modelID, filename)
	fmt.Fprintf(w, "Downloading %s from %s...\n", filename, modelID)

	resp, err := http.Get(dlURL)
	if err != nil {
		return "", fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download failed: HTTP %d for %s", resp.StatusCode, dlURL)
	}

	tmp := dest + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return "", err
	}

	total := resp.ContentLength
	var written int64
	buf := make([]byte, 1<<20) // 1MB chunks
	lastPct := -1
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				f.Close()
				os.Remove(tmp)
				return "", werr
			}
			written += int64(n)
			if total > 0 {
				pct := int(written * 100 / total)
				if pct != lastPct && pct%10 == 0 {
					fmt.Fprintf(w, "  %d%% (%.1f / %.1f GB)\n",
						pct, float64(written)/1e9, float64(total)/1e9)
					lastPct = pct
				}
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			f.Close()
			os.Remove(tmp)
			return "", fmt.Errorf("download error: %w", err)
		}
	}
	f.Close()

	if err := os.Rename(tmp, dest); err != nil {
		return "", err
	}

	fmt.Fprintf(w, "Downloaded to %s\n", dest)
	return dest, nil
}
