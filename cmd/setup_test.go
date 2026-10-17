package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPickLlamaAsset_SkipsReleasesWithoutUbuntuBinary(t *testing.T) {
	releases := []release{
		{TagName: "v0.7.0"},
		{TagName: "b11706", Assets: []asset{
			{Name: "llama-b11706-bin-ubuntu-vulkan-x64.tar.gz", BrowserDownloadURL: "https://x/vulkan"},
			{Name: "llama-b11706-bin-ubuntu-x64.tar.gz", BrowserDownloadURL: "https://x/cpu"},
		}},
		{TagName: "b11705", Assets: []asset{
			{Name: "llama-b11705-bin-ubuntu-x64.tar.gz", BrowserDownloadURL: "https://x/old"},
		}},
	}
	url, name, err := pickLlamaAsset(releases)
	if err != nil {
		t.Fatal(err)
	}
	if url != "https://x/cpu" || name != "llama-b11706-bin-ubuntu-x64.tar.gz" {
		t.Errorf("got %s %s", url, name)
	}
}

func TestPickLlamaAsset_ErrorsWhenNothingMatches(t *testing.T) {
	if _, _, err := pickLlamaAsset([]release{{TagName: "v0.7.0"}}); err == nil {
		t.Error("want error")
	}
}

func TestLlamaInstallDir_FollowsExistingSymlink(t *testing.T) {
	root := t.TempDir()
	libDir := filepath.Join(root, "lib", "llama-cpp")
	binDir := filepath.Join(root, "bin")
	for _, d := range []string{libDir, binDir} {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatal(err)
		}
	}
	real := filepath.Join(libDir, "llama-server")
	if err := os.WriteFile(real, nil, 0755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(binDir, "llama-server")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	got, err := llamaInstallDir(link, "/home/u")
	if err != nil {
		t.Fatal(err)
	}
	want, _ := filepath.EvalSymlinks(libDir)
	if got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestLlamaInstallDir_DefaultsUnderHomeWhenNotInstalled(t *testing.T) {
	got, err := llamaInstallDir("", "/home/u")
	if err != nil {
		t.Fatal(err)
	}
	if got != "/home/u/.local/lib/llama-cpp" {
		t.Errorf("got %s", got)
	}
}
