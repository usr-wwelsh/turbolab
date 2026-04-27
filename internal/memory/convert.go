package memory

import (
	"bytes"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

func ConvertToMarkdown(filename string, data []byte) (string, error) {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".md", ".markdown":
		return string(data), nil
	case ".txt":
		return string(data), nil
	}
	from := extToFormat(ext)
	cmd := exec.Command("pandoc", "--from", from, "--to", "gfm")
	cmd.Stdin = bytes.NewReader(data)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("pandoc conversion failed (install pandoc for %s support): %v", ext, err)
	}
	return string(out), nil
}

func extToFormat(ext string) string {
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
	default:
		return "plain"
	}
}
