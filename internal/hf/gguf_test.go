package hf

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSelectMMProjPrefersDefaultOverVariant(t *testing.T) {
	files := []ModelFile{
		{Filename: "mmproj-model-f16-4x.gguf"},
		{Filename: "mmproj-model-f16.gguf"},
	}
	if got := SelectMMProj(files); got != "mmproj-model-f16.gguf" {
		t.Errorf("got %q, want mmproj-model-f16.gguf", got)
	}
}

func TestFindMMProjInDirPrefersDefaultOverVariant(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"mmproj-model-f16-4x.gguf", "mmproj-model-f16.gguf"} {
		if err := os.WriteFile(filepath.Join(dir, n), nil, 0644); err != nil {
			t.Fatal(err)
		}
	}
	if got := FindMMProjInDir(dir); filepath.Base(got) != "mmproj-model-f16.gguf" {
		t.Errorf("got %q, want mmproj-model-f16.gguf", got)
	}
}
