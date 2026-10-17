package cmd

import "testing"

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
