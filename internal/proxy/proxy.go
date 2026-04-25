package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"
)

type RecordFunc func(model string, promptTokens, completionTokens int)

func New(targetPort int, record RecordFunc) http.Handler {
	target, _ := url.Parse(fmt.Sprintf("http://localhost:%d", targetPort))
	base := http.DefaultTransport.(*http.Transport).Clone()
	base.ResponseHeaderTimeout = 15 * time.Minute
	rp := &httputil.ReverseProxy{
		Transport: base,
		Director: func(r *http.Request) {
			r.URL.Scheme = target.Scheme
			r.URL.Host = target.Host
			r.Host = target.Host
			log.Printf("proxy → %s %s", r.Method, r.URL.Path)
		},
		FlushInterval: -1,
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			if r.Context().Err() != nil {
				return
			}
			log.Printf("proxy error: %v", err)
			http.Error(w, "model server unavailable: "+err.Error(), http.StatusBadGateway)
		},
		ModifyResponse: func(resp *http.Response) error {
			log.Printf("proxy ← %d %s", resp.StatusCode, resp.Request.URL.Path)
			if record == nil || resp.StatusCode != http.StatusOK {
				return nil
			}
			ct := resp.Header.Get("Content-Type")
			if strings.Contains(ct, "text/event-stream") {
				resp.Body = &streamCapture{ReadCloser: resp.Body, record: record}
			} else if strings.Contains(ct, "application/json") {
				body, err := io.ReadAll(resp.Body)
				resp.Body.Close()
				resp.Body = io.NopCloser(bytes.NewReader(body))
				if err == nil {
					recordFromJSON(body, record)
				}
			}
			return nil
		},
	}
	return rp
}

// streamCapture wraps the SSE response body, passes all bytes through
// unchanged, and extracts usage from the final data chunk.
type streamCapture struct {
	io.ReadCloser
	buf   bytes.Buffer
	record RecordFunc
	fired bool
}

func (s *streamCapture) Read(p []byte) (n int, err error) {
	n, err = s.ReadCloser.Read(p)
	if n > 0 {
		s.buf.Write(p[:n])
	}
	if err == io.EOF && !s.fired {
		s.fired = true
		s.parse()
	}
	return
}

// parse scans the accumulated SSE buffer for token usage.
// It prefers server-provided usage fields; falls back to counting delta chunks
// so something is always recorded even if the server omits stream_options usage.
func (s *streamCapture) parse() {
	var model string
	var promptTokens, completionTokens int
	deltaCount := 0 // number of chunks that contained actual content

	for _, line := range strings.Split(s.buf.String(), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data: ") || line == "data: [DONE]" {
			continue
		}
		var chunk struct {
			Model   string `json:"model"`
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
			Usage *struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
			} `json:"usage"`
		}
		if json.Unmarshal([]byte(line[6:]), &chunk) != nil {
			continue
		}
		if chunk.Model != "" {
			model = chunk.Model
		}
		if len(chunk.Choices) > 0 && chunk.Choices[0].Delta.Content != "" {
			deltaCount++
		}
		// Server-provided usage takes priority; keep the last non-zero reading
		if chunk.Usage != nil && (chunk.Usage.PromptTokens > 0 || chunk.Usage.CompletionTokens > 0) {
			promptTokens = chunk.Usage.PromptTokens
			completionTokens = chunk.Usage.CompletionTokens
		}
	}

	// Fall back to delta-chunk count when server doesn't report usage
	if completionTokens == 0 && deltaCount > 0 {
		completionTokens = deltaCount
	}
	if completionTokens > 0 || promptTokens > 0 {
		s.record(model, promptTokens, completionTokens)
	}
}

func recordFromJSON(body []byte, record RecordFunc) {
	var resp struct {
		Model string `json:"model"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if json.Unmarshal(body, &resp) == nil && (resp.Usage.PromptTokens > 0 || resp.Usage.CompletionTokens > 0) {
		record(resp.Model, resp.Usage.PromptTokens, resp.Usage.CompletionTokens)
	}
}
