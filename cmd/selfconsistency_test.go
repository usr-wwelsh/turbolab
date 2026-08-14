package cmd

import (
	"encoding/json"
	"testing"
)

func mkBody(t *testing.T, content string) []byte {
	t.Helper()
	b, err := json.Marshal(map[string]any{
		"choices": []map[string]any{
			{"message": map[string]any{"content": content}},
		},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func TestSelectByVote(t *testing.T) {
	cases := []struct {
		name    string
		bodies  [][]byte
		want    string // expected content of the winning body
		wantErr bool
	}{
		{
			name:   "clear majority",
			bodies: [][]byte{mkBody(t, "Paris"), mkBody(t, "Paris"), mkBody(t, "London")},
			want:   "Paris",
		},
		{
			name:   "tie broken by first occurrence",
			bodies: [][]byte{mkBody(t, "A"), mkBody(t, "B"), mkBody(t, "A"), mkBody(t, "B")},
			want:   "A",
		},
		{
			name:   "all distinct falls back to first",
			bodies: [][]byte{mkBody(t, "one"), mkBody(t, "two"), mkBody(t, "three")},
			want:   "one",
		},
		{
			name:   "single response",
			bodies: [][]byte{mkBody(t, "only answer")},
			want:   "only answer",
		},
		{
			name:   "malformed body skipped",
			bodies: [][]byte{[]byte("not json"), mkBody(t, "42"), mkBody(t, "42")},
			want:   "42",
		},
		{
			name:   "empty choices skipped",
			bodies: [][]byte{[]byte(`{"choices":[]}`), mkBody(t, "42"), mkBody(t, "42")},
			want:   "42",
		},
		{
			name:    "all bodies malformed errors",
			bodies:  [][]byte{[]byte("not json"), []byte(`{}`)},
			wantErr: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			winner, err := selectByVote(c.bodies)
			if c.wantErr {
				if err == nil {
					t.Fatalf("expected error, got winner %s", winner)
				}
				if winner != nil {
					t.Fatalf("expected nil winner on error, got %s", winner)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			var resp struct {
				Choices []struct {
					Message struct {
						Content string `json:"content"`
					} `json:"message"`
				} `json:"choices"`
			}
			if err := json.Unmarshal(winner, &resp); err != nil {
				t.Fatalf("winner not valid json: %v", err)
			}
			if len(resp.Choices) == 0 || resp.Choices[0].Message.Content != c.want {
				t.Fatalf("got %q, want %q", winner, c.want)
			}
		})
	}
}

func TestSelectByVote_NormalizationMerges(t *testing.T) {
	spacedParis := mkBody(t, "  Paris  ")
	bodies := [][]byte{spacedParis, mkBody(t, "PARIS"), mkBody(t, "London")}

	winner, err := selectByVote(bodies)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(winner) != string(spacedParis) {
		t.Fatalf("expected raw body of first merged candidate, got %s want %s", winner, spacedParis)
	}
}

func TestSummarizeCandidates(t *testing.T) {
	bodies := [][]byte{mkBody(t, "Paris"), mkBody(t, "Paris"), mkBody(t, "London"), []byte("not json")}
	winner, err := selectByVote(bodies)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	summary := summarizeCandidates(bodies, winner)
	if len(summary) != 2 {
		t.Fatalf("expected 2 groups (malformed body skipped), got %d: %+v", len(summary), summary)
	}

	byContent := map[string]selfConsistencyCandidate{}
	for _, s := range summary {
		byContent[s.Content] = s
	}

	paris, ok := byContent["Paris"]
	if !ok {
		t.Fatalf("expected a Paris group, got %+v", summary)
	}
	if paris.Votes != 2 || !paris.Chosen {
		t.Fatalf("expected Paris to have 2 votes and be chosen, got %+v", paris)
	}

	london, ok := byContent["London"]
	if !ok {
		t.Fatalf("expected a London group, got %+v", summary)
	}
	if london.Votes != 1 || london.Chosen {
		t.Fatalf("expected London to have 1 vote and not be chosen, got %+v", london)
	}
}

func TestAttachCandidates(t *testing.T) {
	raw := mkBody(t, "Paris")
	cands := []selfConsistencyCandidate{{Content: "Paris", Votes: 2, Chosen: true}, {Content: "London", Votes: 1}}

	out := attachCandidates(raw, cands)

	var obj map[string]any
	if err := json.Unmarshal(out, &obj); err != nil {
		t.Fatalf("output not valid json: %v", err)
	}
	got, ok := obj["self_consistency_candidates"]
	if !ok {
		t.Fatalf("expected self_consistency_candidates field, got %v", obj)
	}
	list, ok := got.([]any)
	if !ok || len(list) != 2 {
		t.Fatalf("expected 2 candidates, got %v", got)
	}
	// original fields still present
	if _, ok := obj["choices"]; !ok {
		t.Fatalf("expected original choices field preserved, got %v", obj)
	}
}

func TestAttachCandidates_EmptyReturnsUnchanged(t *testing.T) {
	raw := mkBody(t, "Paris")
	out := attachCandidates(raw, nil)
	if string(out) != string(raw) {
		t.Fatalf("expected unchanged body when candidates empty, got %s want %s", out, raw)
	}
}

func TestNormalizeAnswer(t *testing.T) {
	cases := []struct{ in, want string }{
		{"  Paris  ", "paris"},
		{"PARIS", "paris"},
		{"the   cat\nsat", "the cat sat"},
		{"", ""},
	}
	for _, c := range cases {
		if got := normalizeAnswer(c.in); got != c.want {
			t.Errorf("normalizeAnswer(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
