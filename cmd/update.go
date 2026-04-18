package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"

	"github.com/spf13/cobra"
)

type release struct {
	TagName string  `json:"tag_name"`
	Assets  []asset `json:"assets"`
}

type asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update turbolab to the latest release",
	RunE:  runUpdate,
}

func releaseAPI() string {
	if u := os.Getenv("TURBOLAB_RELEASE_API"); u != "" {
		return u
	}
	return "https://api.github.com/repos/usr-wwelsh/turbolab/releases/latest"
}

func runUpdate(cmd *cobra.Command, args []string) error {
	fmt.Println("Checking for updates...")

	req, _ := http.NewRequest("GET", releaseAPI(), nil)
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to reach release server: %w", err)
	}
	defer resp.Body.Close()

	var rel release
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return fmt.Errorf("failed to parse release: %w", err)
	}

	if rel.TagName == version {
		fmt.Printf("Already up to date (%s)\n", version)
		return nil
	}

	fmt.Printf("New version available: %s (current: %s)\n", rel.TagName, version)

	assetName := fmt.Sprintf("turbolab_%s_%s", runtime.GOOS, runtime.GOARCH)
	var downloadURL string
	for _, a := range rel.Assets {
		if a.Name == assetName {
			downloadURL = a.BrowserDownloadURL
			break
		}
	}
	if downloadURL == "" {
		return fmt.Errorf("no binary found in latest release")
	}

	self, err := os.Executable()
	if err != nil {
		return err
	}

	fmt.Printf("Downloading %s...\n", downloadURL)

	tmp, err := os.CreateTemp(filepath.Dir(self), "turbolab-update-*")
	if err != nil {
		return fmt.Errorf("replace failed (try sudo?): %w", err)
	}
	defer os.Remove(tmp.Name())

	dlResp, err := http.Get(downloadURL)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer dlResp.Body.Close()

	if _, err := io.Copy(tmp, dlResp.Body); err != nil {
		tmp.Close()
		return fmt.Errorf("write failed: %w", err)
	}
	tmp.Close()

	if err := os.Chmod(tmp.Name(), 0755); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), self); err != nil {
		return fmt.Errorf("replace failed (try sudo?): %w", err)
	}

	fmt.Printf("Updated to %s\n", rel.TagName)
	return nil
}
