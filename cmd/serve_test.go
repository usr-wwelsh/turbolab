package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/usr-wwelsh/turbolab/internal/config"
	"github.com/usr-wwelsh/turbolab/internal/process"
	"github.com/usr-wwelsh/turbolab/internal/proxy"
)

func newTestManager(embedding bool) *process.Manager {
	return process.New("", "", io.Discard, 0, 4, 0, 2048, true, false, embedding)
}

// newSelfConsistencyTestDeps wires backendURL at the self-consistency/MCP
// sub-request client, and a distinct counting stub as the fallthrough proxy,
// so tests can tell "fanned out to the backend" apart from "fell through to
// the raw proxy".
func newSelfConsistencyTestDeps(backendURL string, cfg config.Config) (chatHandlerDeps, *int32) {
	var proxyHits int32
	proxyStub := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&proxyHits, 1)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"content":"proxied"}}]}`))
	})
	return chatHandlerDeps{
		mgr:            newTestManager(false),
		idMgr:          newTestManager(true),
		backendURL:     backendURL,
		backendClient:  &http.Client{},
		inferenceProxy: proxyStub,
		mcpOn:          &atomic.Bool{},
		mcpSrv:         nil,
		memDB:          nil,
		memInjectOn:    &atomic.Bool{},
		convTracker:    newConvTracker(16),
		recordFn:       nil,
		loadConfig:     func() (config.Config, error) { return cfg, nil },
	}, &proxyHits
}

// newPassthroughTestDeps wires inferenceProxy as a real reverse proxy to the
// given backend, for cases (like CoT) that only mutate the payload and rely
// on the normal single-shot proxy path — so the backend actually sees what
// the handler sent it.
func newPassthroughTestDeps(t *testing.T, backend *httptest.Server, cfg config.Config) chatHandlerDeps {
	t.Helper()
	addr, ok := backend.Listener.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("unexpected listener addr type")
	}
	return chatHandlerDeps{
		mgr:            newTestManager(false),
		idMgr:          newTestManager(true),
		backendURL:     backend.URL,
		backendClient:  &http.Client{},
		inferenceProxy: proxy.New(addr.Port, nil),
		mcpOn:          &atomic.Bool{},
		mcpSrv:         nil,
		memDB:          nil,
		memInjectOn:    &atomic.Bool{},
		convTracker:    newConvTracker(16),
		recordFn:       nil,
		loadConfig:     func() (config.Config, error) { return cfg, nil },
	}
}

func doChatRequest(t *testing.T, handler http.HandlerFunc, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader([]byte(body)))
	rec := httptest.NewRecorder()
	handler(rec, req)
	return rec
}

func TestChatCompletions_SelfConsistency_MajorityVote(t *testing.T) {
	var callCount int32
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&callCount, 1)
		content := "B"
		if n <= 2 {
			content = "A"
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]any{"content": content}}},
		})
	}))
	defer backend.Close()

	deps, proxyHits := newSelfConsistencyTestDeps(backend.URL, config.Config{SelfConsistencyN: 3})
	handler := newChatCompletionsHandler(deps)

	rec := doChatRequest(t, handler, `{"model":"default","messages":[{"role":"user","content":"capital of France?"}]}`)

	if got := atomic.LoadInt32(&callCount); got != 3 {
		t.Fatalf("expected 3 backend calls, got %d", got)
	}
	if got := atomic.LoadInt32(proxyHits); got != 0 {
		t.Fatalf("expected no proxy fallthrough, got %d hits", got)
	}

	var resp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("bad response json: %v", err)
	}
	if len(resp.Choices) == 0 || resp.Choices[0].Message.Content != "A" {
		t.Fatalf("expected winning content %q, got %+v", "A", resp)
	}
}

func TestChatCompletions_SelfConsistency_AllBackendErrorsFallsThrough(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer backend.Close()

	deps, proxyHits := newSelfConsistencyTestDeps(backend.URL, config.Config{SelfConsistencyN: 3})
	handler := newChatCompletionsHandler(deps)

	doChatRequest(t, handler, `{"model":"default","messages":[{"role":"user","content":"hi"}]}`)

	if got := atomic.LoadInt32(proxyHits); got != 1 {
		t.Fatalf("expected exactly 1 proxy fallthrough, got %d", got)
	}
}

func TestChatCompletions_SelfConsistency_SkippedWhenClientHasTools(t *testing.T) {
	var callCount int32
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer backend.Close()

	deps, proxyHits := newSelfConsistencyTestDeps(backend.URL, config.Config{SelfConsistencyN: 3})
	handler := newChatCompletionsHandler(deps)

	body := `{"model":"default","messages":[{"role":"user","content":"hi"}],"tools":[{"type":"function","function":{"name":"foo"}}]}`
	doChatRequest(t, handler, body)

	if got := atomic.LoadInt32(&callCount); got != 0 {
		t.Fatalf("expected no self-consistency backend calls when client supplies tools, got %d", got)
	}
	if got := atomic.LoadInt32(proxyHits); got != 1 {
		t.Fatalf("expected exactly 1 proxy fallthrough, got %d", got)
	}
}

func TestChatCompletions_SelfConsistency_TemperatureDefaultedAndRespected(t *testing.T) {
	var mu sync.Mutex
	var gotTemps []float64
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		if temp, ok := body["temperature"].(float64); ok {
			gotTemps = append(gotTemps, temp)
		}
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer backend.Close()

	deps, _ := newSelfConsistencyTestDeps(backend.URL, config.Config{SelfConsistencyN: 2})
	handler := newChatCompletionsHandler(deps)

	doChatRequest(t, handler, `{"model":"default","messages":[{"role":"user","content":"hi"}]}`)

	mu.Lock()
	if len(gotTemps) != 2 {
		t.Fatalf("expected 2 recorded temperatures, got %d", len(gotTemps))
	}
	for _, temp := range gotTemps {
		if temp != selfConsistencyDefaultTemp {
			t.Fatalf("expected default temperature %v when unset, got %v", selfConsistencyDefaultTemp, temp)
		}
	}
	gotTemps = nil
	mu.Unlock()

	doChatRequest(t, handler, `{"model":"default","temperature":1.2,"messages":[{"role":"user","content":"hi"}]}`)

	mu.Lock()
	defer mu.Unlock()
	if len(gotTemps) != 2 {
		t.Fatalf("expected 2 recorded temperatures, got %d", len(gotTemps))
	}
	for _, temp := range gotTemps {
		if temp != 1.2 {
			t.Fatalf("expected client temperature 1.2 to be respected, got %v", temp)
		}
	}
}

func TestChatCompletions_SelfConsistency_ShowAllCandidates(t *testing.T) {
	var callCount int32
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&callCount, 1)
		content := "B"
		if n <= 2 {
			content = "A"
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]any{"content": content}}},
		})
	}))
	defer backend.Close()

	deps, _ := newSelfConsistencyTestDeps(backend.URL, config.Config{SelfConsistencyN: 3, SelfConsistencyShowAll: true})
	handler := newChatCompletionsHandler(deps)

	rec := doChatRequest(t, handler, `{"model":"default","messages":[{"role":"user","content":"capital of France?"}]}`)

	var resp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		SelfConsistencyCandidates []selfConsistencyCandidate `json:"self_consistency_candidates"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("bad response json: %v", err)
	}
	if len(resp.Choices) == 0 || resp.Choices[0].Message.Content != "A" {
		t.Fatalf("expected winning content A, got %+v", resp)
	}
	if len(resp.SelfConsistencyCandidates) != 2 {
		t.Fatalf("expected 2 candidate groups (A, B), got %+v", resp.SelfConsistencyCandidates)
	}
	var foundChosenA bool
	for _, c := range resp.SelfConsistencyCandidates {
		if c.Content == "A" {
			if c.Votes != 2 || !c.Chosen {
				t.Fatalf("expected A to have 2 votes and be chosen, got %+v", c)
			}
			foundChosenA = true
		}
	}
	if !foundChosenA {
		t.Fatalf("expected an A candidate in %+v", resp.SelfConsistencyCandidates)
	}
}

func TestChatCompletions_SelfConsistency_ShowAllOff_NoCandidatesField(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"content":"A"}}]}`))
	}))
	defer backend.Close()

	deps, _ := newSelfConsistencyTestDeps(backend.URL, config.Config{SelfConsistencyN: 3, SelfConsistencyShowAll: false})
	handler := newChatCompletionsHandler(deps)

	rec := doChatRequest(t, handler, `{"model":"default","messages":[{"role":"user","content":"hi"}]}`)

	var obj map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &obj); err != nil {
		t.Fatalf("bad response json: %v", err)
	}
	if _, ok := obj["self_consistency_candidates"]; ok {
		t.Fatalf("expected no self_consistency_candidates field when show-all is off, got %v", obj)
	}
}

func TestChatCompletions_SelfConsistency_ShowAllCandidates_Streaming(t *testing.T) {
	var callCount int32
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&callCount, 1)
		content := "B"
		if n <= 2 {
			content = "A"
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]any{"content": content, "finish_reason": "stop"}}},
		})
	}))
	defer backend.Close()

	deps, _ := newSelfConsistencyTestDeps(backend.URL, config.Config{SelfConsistencyN: 3, SelfConsistencyShowAll: true})
	handler := newChatCompletionsHandler(deps)

	rec := doChatRequest(t, handler, `{"model":"default","stream":true,"messages":[{"role":"user","content":"capital of France?"}]}`)

	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("expected SSE content type, got %q", ct)
	}
	if !strings.Contains(rec.Body.String(), `"self_consistency_candidates"`) {
		t.Fatalf("expected self_consistency_candidates in SSE stream, got %s", rec.Body.String())
	}
}

func TestChatCompletions_SystemPrompt_InjectsWhenAbsent(t *testing.T) {
	var gotMessages []any
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		gotMessages, _ = body["messages"].([]any)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer backend.Close()

	deps := newPassthroughTestDeps(t, backend, config.Config{SystemPrompt: "You are a terse pirate."})
	handler := newChatCompletionsHandler(deps)

	doChatRequest(t, handler, `{"model":"default","messages":[{"role":"user","content":"hi"}]}`)

	if len(gotMessages) != 2 {
		t.Fatalf("expected system message injected + user message, got %d: %+v", len(gotMessages), gotMessages)
	}
	first, _ := gotMessages[0].(map[string]any)
	if role, _ := first["role"].(string); role != "system" {
		t.Fatalf("expected first message to be system, got %+v", first)
	}
	if content, _ := first["content"].(string); content != "You are a terse pirate." {
		t.Fatalf("expected default system prompt, got %q", content)
	}
}

func TestChatCompletions_SystemPrompt_RespectsClientSuppliedSystemMessage(t *testing.T) {
	var gotMessages []any
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		gotMessages, _ = body["messages"].([]any)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer backend.Close()

	deps := newPassthroughTestDeps(t, backend, config.Config{SystemPrompt: "You are a terse pirate."})
	handler := newChatCompletionsHandler(deps)

	body := `{"model":"default","messages":[{"role":"system","content":"Client persona."},{"role":"user","content":"hi"}]}`
	doChatRequest(t, handler, body)

	if len(gotMessages) != 2 {
		t.Fatalf("expected 2 messages (no extra system message injected), got %d: %+v", len(gotMessages), gotMessages)
	}
	first, _ := gotMessages[0].(map[string]any)
	if content, _ := first["content"].(string); content != "Client persona." {
		t.Fatalf("expected client's own system message to win, got %q", content)
	}
}

func TestChatCompletions_SystemPrompt_EmptyNoInjection(t *testing.T) {
	var gotMessages []any
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		gotMessages, _ = body["messages"].([]any)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer backend.Close()

	deps := newPassthroughTestDeps(t, backend, config.Config{})
	handler := newChatCompletionsHandler(deps)

	doChatRequest(t, handler, `{"model":"default","messages":[{"role":"user","content":"hi"}]}`)

	if len(gotMessages) != 1 {
		t.Fatalf("expected 1 message (no system message injected), got %d", len(gotMessages))
	}
}

func TestChatCompletions_CoT_InjectsAndPrependsToExistingSystemMessage(t *testing.T) {
	var gotMessages []any
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		gotMessages, _ = body["messages"].([]any)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer backend.Close()

	deps := newPassthroughTestDeps(t, backend, config.Config{CoTPromptEnabled: true})
	handler := newChatCompletionsHandler(deps)

	body := `{"model":"default","messages":[{"role":"system","content":"You are a helpful assistant."},{"role":"user","content":"hi"}]}`
	doChatRequest(t, handler, body)

	if len(gotMessages) == 0 {
		t.Fatalf("backend received no messages")
	}
	first, _ := gotMessages[0].(map[string]any)
	content, _ := first["content"].(string)
	if !strings.Contains(content, cotInstruction) {
		t.Fatalf("expected CoT instruction appended to system message, got %q", content)
	}
	if !strings.HasPrefix(content, "You are a helpful assistant.") {
		t.Fatalf("expected existing system content preserved first, got %q", content)
	}
}

func TestChatCompletions_CoT_DisabledNoInjection(t *testing.T) {
	var gotMessages []any
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		gotMessages, _ = body["messages"].([]any)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer backend.Close()

	deps := newPassthroughTestDeps(t, backend, config.Config{CoTPromptEnabled: false})
	handler := newChatCompletionsHandler(deps)

	doChatRequest(t, handler, `{"model":"default","messages":[{"role":"user","content":"hi"}]}`)

	if len(gotMessages) != 1 {
		t.Fatalf("expected 1 message (no system message injected), got %d", len(gotMessages))
	}
}

func ptrF64(v float64) *float64 { return &v }
func ptrInt(v int) *int         { return &v }
func ptrI64(v int64) *int64     { return &v }

func TestChatCompletions_SamplingDefaults_InjectedWhenAbsent(t *testing.T) {
	var gotBody map[string]any
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer backend.Close()

	cfg := config.Config{
		Temperature:   ptrF64(0.7),
		TopP:          ptrF64(0.9),
		TopK:          ptrInt(40),
		MinP:          ptrF64(0.05),
		RepeatPenalty: ptrF64(1.1),
		Seed:          ptrI64(42),
	}
	deps := newPassthroughTestDeps(t, backend, cfg)
	handler := newChatCompletionsHandler(deps)

	doChatRequest(t, handler, `{"model":"default","messages":[{"role":"user","content":"hi"}]}`)

	want := map[string]float64{"temperature": 0.7, "top_p": 0.9, "top_k": 40, "min_p": 0.05, "repeat_penalty": 1.1, "seed": 42}
	for k, v := range want {
		got, ok := gotBody[k].(float64)
		if !ok {
			t.Fatalf("expected backend to receive %q, got body %+v", k, gotBody)
		}
		if got != v {
			t.Fatalf("expected %q = %v, got %v", k, v, got)
		}
	}
}

func TestChatCompletions_SamplingDefaults_ClientSuppliedWins(t *testing.T) {
	var gotBody map[string]any
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer backend.Close()

	deps := newPassthroughTestDeps(t, backend, config.Config{Temperature: ptrF64(0.7), TopK: ptrInt(40)})
	handler := newChatCompletionsHandler(deps)

	doChatRequest(t, handler, `{"model":"default","messages":[{"role":"user","content":"hi"}],"temperature":0.2,"top_k":10}`)

	if got := gotBody["temperature"]; got != 0.2 {
		t.Fatalf("expected client's temperature to win, got %v", got)
	}
	if got := gotBody["top_k"]; got != float64(10) {
		t.Fatalf("expected client's top_k to win, got %v", got)
	}
}

func TestChatCompletions_SamplingDefaults_UnsetFieldsNotInjected(t *testing.T) {
	var gotBody map[string]any
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer backend.Close()

	deps := newPassthroughTestDeps(t, backend, config.Config{})
	handler := newChatCompletionsHandler(deps)

	doChatRequest(t, handler, `{"model":"default","messages":[{"role":"user","content":"hi"}]}`)

	for _, k := range []string{"temperature", "top_p", "top_k", "min_p", "repeat_penalty", "seed"} {
		if _, ok := gotBody[k]; ok {
			t.Fatalf("expected %q to be absent when unconfigured, got %+v", k, gotBody)
		}
	}
}
