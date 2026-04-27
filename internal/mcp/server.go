package mcp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"

	"github.com/usr-wwelsh/turbolab/internal/memory"
)

type Server struct {
	mem     *memory.DB
	enabled *atomic.Bool
}

func New(mem *memory.DB, enabled *atomic.Bool) *Server {
	return &Server{mem: mem, enabled: enabled}
}

func (s *Server) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.enabled.Load() {
			http.Error(w, "MCP disabled", http.StatusServiceUnavailable)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      any             `json:"id"`
			Method  string          `json:"method"`
			Params  json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		s.dispatch(w, req.ID, req.Method, req.Params)
	}
}

func (s *Server) dispatch(w http.ResponseWriter, id any, method string, params json.RawMessage) {
	switch method {
	case "initialize":
		respond(w, id, map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "turbolab-memory", "version": "1.0"},
		}, nil)
	case "notifications/initialized", "ping":
		respond(w, id, map[string]any{}, nil)
	case "tools/list":
		respond(w, id, map[string]any{"tools": toolList()}, nil)
	case "tools/call":
		result, err := s.callTool(params)
		if err != nil {
			respond(w, id, nil, &rpcError{Code: -32603, Message: err.Error()})
		} else {
			respond(w, id, result, nil)
		}
	default:
		respond(w, id, nil, &rpcError{Code: -32601, Message: "method not found: " + method})
	}
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func respond(w http.ResponseWriter, id any, result any, e *rpcError) {
	resp := map[string]any{"jsonrpc": "2.0", "id": id}
	if e != nil {
		resp["error"] = e
	} else {
		resp["result"] = result
	}
	json.NewEncoder(w).Encode(resp)
}

func text(t string) any {
	return map[string]any{"content": []map[string]any{{"type": "text", "text": t}}}
}

func (s *Server) callTool(params json.RawMessage) (any, error) {
	var p struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params")
	}

	switch p.Name {
	case "search_memory":
		var args struct {
			Query string `json:"query"`
			Limit int    `json:"limit"`
		}
		json.Unmarshal(p.Arguments, &args)
		if args.Limit <= 0 {
			args.Limit = 5
		}
		mems, err := s.mem.Search(args.Query, args.Limit)
		if err != nil {
			return nil, err
		}
		return text(formatMemories(mems)), nil

	case "add_memory":
		var args struct {
			Content string   `json:"content"`
			Source  string   `json:"source"`
			Tags    []string `json:"tags"`
		}
		json.Unmarshal(p.Arguments, &args)
		m, err := s.mem.Add(args.Content, args.Source, args.Tags)
		if err != nil {
			return nil, err
		}
		return text("Memory added: " + m.ID), nil

	case "relate_memories":
		var args struct {
			FromID  string `json:"from_id"`
			ToID    string `json:"to_id"`
			RelType string `json:"rel_type"`
		}
		json.Unmarshal(p.Arguments, &args)
		if err := s.mem.Relate(args.FromID, args.ToID, args.RelType); err != nil {
			return nil, err
		}
		return text("Relationship created"), nil

	case "get_related":
		var args struct {
			MemoryID string `json:"memory_id"`
			Depth    int    `json:"depth"`
		}
		json.Unmarshal(p.Arguments, &args)
		mems, edges, err := s.mem.GetRelated(args.MemoryID, args.Depth)
		if err != nil {
			return nil, err
		}
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("Found %d related memories:\n\n", len(mems)))
		for _, m := range mems {
			sb.WriteString(fmt.Sprintf("[%s] %s\n", m.ID[:8], m.Content))
		}
		sb.WriteString(fmt.Sprintf("\n%d edges\n", len(edges)))
		return text(sb.String()), nil

	case "semantic_search_memory":
		var args struct {
			Query    string  `json:"query"`
			Limit    int     `json:"limit"`
			MinScore float32 `json:"min_score"`
		}
		json.Unmarshal(p.Arguments, &args)
		if args.Limit <= 0 {
			args.Limit = 5
		}
		if args.MinScore <= 0 {
			args.MinScore = 0.3
		}
		scored, err := s.mem.SemanticSearch(args.Query, args.Limit, args.MinScore)
		if err != nil {
			return nil, err
		}
		if len(scored) == 0 {
			return text("No semantically similar memories found."), nil
		}
		var sb strings.Builder
		for _, s := range scored {
			sb.WriteString(fmt.Sprintf("[%s] (%.2f) %s\n\n", s.ID[:8], s.Score, s.Content))
		}
		return text(strings.TrimSpace(sb.String())), nil

	default:
		return nil, fmt.Errorf("unknown tool: %s", p.Name)
	}
}

func formatMemories(mems []memory.Memory) string {
	if len(mems) == 0 {
		return "No memories found."
	}
	var sb strings.Builder
	for _, m := range mems {
		sb.WriteString(fmt.Sprintf("[%s] %s", m.ID[:8], m.Content))
		if m.Source != "" {
			sb.WriteString(fmt.Sprintf(" (source: %s)", m.Source))
		}
		sb.WriteString("\n\n")
	}
	return strings.TrimSpace(sb.String())
}

// ToolDefs returns tool definitions in OpenAI function-calling format.
func (s *Server) ToolDefs() []map[string]any {
	out := make([]map[string]any, 0)
	for _, t := range toolList() {
		out = append(out, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        t["name"],
				"description": t["description"],
				"parameters":  t["inputSchema"],
			},
		})
	}
	return out
}

// CallTool executes a named tool and returns its text result.
func (s *Server) CallTool(name string, args json.RawMessage) (string, error) {
	raw, _ := json.Marshal(map[string]any{"name": name, "arguments": args})
	result, err := s.callTool(raw)
	if err != nil {
		return "", err
	}
	if m, ok := result.(map[string]any); ok {
		if content, ok := m["content"].([]map[string]any); ok && len(content) > 0 {
			if t, ok := content[0]["text"].(string); ok {
				return t, nil
			}
		}
	}
	b, _ := json.Marshal(result)
	return string(b), nil
}

func toolList() []map[string]any {
	return []map[string]any{
		{
			"name":        "search_memory",
			"description": "Search memories by keyword",
			"inputSchema": map[string]any{
				"type":     "object",
				"required": []string{"query"},
				"properties": map[string]any{
					"query": map[string]any{"type": "string", "description": "Search query"},
					"limit": map[string]any{"type": "integer", "description": "Max results (default 5)"},
				},
			},
		},
		{
			"name":        "add_memory",
			"description": "Add a new memory",
			"inputSchema": map[string]any{
				"type":     "object",
				"required": []string{"content"},
				"properties": map[string]any{
					"content": map[string]any{"type": "string"},
					"source":  map[string]any{"type": "string", "description": "Optional source (file, URL, etc.)"},
					"tags":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				},
			},
		},
		{
			"name":        "relate_memories",
			"description": "Create a relationship between two memories",
			"inputSchema": map[string]any{
				"type":     "object",
				"required": []string{"from_id", "to_id"},
				"properties": map[string]any{
					"from_id":  map[string]any{"type": "string"},
					"to_id":    map[string]any{"type": "string"},
					"rel_type": map[string]any{"type": "string", "description": "e.g. uses, depends_on, related"},
				},
			},
		},
		{
			"name":        "get_related",
			"description": "Get memories related to a given memory via graph traversal",
			"inputSchema": map[string]any{
				"type":     "object",
				"required": []string{"memory_id"},
				"properties": map[string]any{
					"memory_id": map[string]any{"type": "string"},
					"depth":     map[string]any{"type": "integer", "description": "Traversal depth (default 1)"},
				},
			},
		},
		{
			"name":        "semantic_search_memory",
			"description": "Find memories by semantic similarity using vector embeddings (requires a loaded model)",
			"inputSchema": map[string]any{
				"type":     "object",
				"required": []string{"query"},
				"properties": map[string]any{
					"query":     map[string]any{"type": "string", "description": "Natural language query"},
					"limit":     map[string]any{"type": "integer", "description": "Max results (default 5)"},
					"min_score": map[string]any{"type": "number", "description": "Minimum cosine similarity 0-1 (default 0.3)"},
				},
			},
		},
	}
}
