package memory

import (
	"bytes"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

func ConvertToMarkdown(filename string, data []byte) (string, error) {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".md", ".markdown", ".txt":
		return string(data), nil
	}
	if from := extToPandocFormat(ext); from != "" {
		cmd := exec.Command("pandoc", "--from", from, "--to", "gfm")
		cmd.Stdin = bytes.NewReader(data)
		out, err := cmd.Output()
		if err != nil {
			return "", fmt.Errorf("pandoc conversion failed (install pandoc for %s support): %v", ext, err)
		}
		return string(out), nil
	}
	// Unknown extension: pass through if plausibly text (utf-8, no NULs).
	if isPlausiblyText(data) {
		lang := strings.TrimPrefix(ext, ".")
		if lang == "" {
			return string(data), nil
		}
		return "```" + lang + "\n" + string(data) + "\n```", nil
	}
	return "", fmt.Errorf("unsupported file type: %s", ext)
}

func extToPandocFormat(ext string) string {
	switch ext {
	case ".html", ".htm":
		return "html"
	case ".pdf":
		return "pdf"
	case ".docx":
		return "docx"
	case ".odt":
		return "odt"
	case ".rst":
		return "rst"
	case ".tex":
		return "latex"
	case ".epub":
		return "epub"
	}
	return ""
}

func isPlausiblyText(data []byte) bool {
	if len(data) == 0 {
		return true
	}
	sample := data
	if len(sample) > 8192 {
		sample = sample[:8192]
	}
	if bytes.IndexByte(sample, 0) >= 0 {
		return false
	}
	return utf8.Valid(sample)
}
