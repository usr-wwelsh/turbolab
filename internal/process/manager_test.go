package process

import (
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestFastCrashLoopGivesUpAfterThreeLaunches(t *testing.T) {
	dir := t.TempDir()
	counter := filepath.Join(dir, "launches")
	bin := filepath.Join(dir, "llama-server")
	script := "#!/bin/sh\necho x >> " + counter + "\nexit 1\n"
	if err := os.WriteFile(bin, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	model := filepath.Join(dir, "m.gguf")
	if err := os.WriteFile(model, nil, 0644); err != nil {
		t.Fatal(err)
	}

	m := New("", bin, io.Discard, 18999, 4, 1, 512, true, false, false)
	m.Start(model)
	time.Sleep(9 * time.Second)

	data, _ := os.ReadFile(counter)
	launches := strings.Count(string(data), "x")
	if launches > 3 {
		t.Fatalf("launched %d times, want at most 3 (got %s)", launches, strconv.Itoa(launches))
	}
}
