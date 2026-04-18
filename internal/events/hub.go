package events

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
)

const recentMax = 200

type Hub struct {
	mu      sync.Mutex
	clients map[chan string]struct{}
	recent  []string
}

func NewHub() *Hub {
	return &Hub{clients: make(map[chan string]struct{})}
}

// Write implements io.Writer — each call broadcasts the line to all SSE clients.
func (h *Hub) Write(p []byte) (int, error) {
	line := strings.TrimRight(string(p), "\n")
	if line == "" {
		return len(p), nil
	}
	h.mu.Lock()
	h.recent = append(h.recent, line)
	if len(h.recent) > recentMax {
		h.recent = h.recent[len(h.recent)-recentMax:]
	}
	for ch := range h.clients {
		select {
		case ch <- line:
		default: // slow client, skip line
		}
	}
	h.mu.Unlock()
	return len(p), nil
}

func (h *Hub) subscribe() chan string {
	ch := make(chan string, 64)
	h.mu.Lock()
	h.clients[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

func (h *Hub) unsubscribe(ch chan string) {
	h.mu.Lock()
	delete(h.clients, ch)
	h.mu.Unlock()
}

func (h *Hub) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		// send recent lines to catch up
		h.mu.Lock()
		recent := make([]string, len(h.recent))
		copy(recent, h.recent)
		h.mu.Unlock()
		for _, line := range recent {
			fmt.Fprintf(w, "data: %s\n\n", line)
		}
		flusher.Flush()

		ch := h.subscribe()
		defer h.unsubscribe(ch)

		for {
			select {
			case line := <-ch:
				fmt.Fprintf(w, "data: %s\n\n", line)
				flusher.Flush()
			case <-r.Context().Done():
				return
			}
		}
	})
}
