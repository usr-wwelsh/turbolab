package cmd

import (
	"encoding/json"
	"errors"
	"strings"
)

// extractContent pulls choices[0].message.content out of a raw chat
// completion response body. ok is false if the body is malformed or has no
// choices.
func extractContent(b []byte) (content string, ok bool) {
	var resp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(b, &resp); err != nil || len(resp.Choices) == 0 {
		return "", false
	}
	return resp.Choices[0].Message.Content, true
}

// selectByVote parses each response body's choices[0].message.content,
// normalizes it, and returns the raw body belonging to the largest group of
// matching normalized answers (majority vote). Malformed bodies or bodies
// with no choices are skipped. Ties are broken by first occurrence in input
// order.
func selectByVote(bodies [][]byte) ([]byte, error) {
	type candidate struct {
		raw  []byte
		norm string
	}

	var candidates []candidate
	for _, b := range bodies {
		content, ok := extractContent(b)
		if !ok {
			continue
		}
		candidates = append(candidates, candidate{
			raw:  b,
			norm: normalizeAnswer(content),
		})
	}

	if len(candidates) == 0 {
		return nil, errors.New("selectByVote: no usable response bodies")
	}

	counts := make(map[string]int)
	firstIdx := make(map[string]int)
	for i, c := range candidates {
		counts[c.norm]++
		if _, ok := firstIdx[c.norm]; !ok {
			firstIdx[c.norm] = i
		}
	}

	bestNorm := candidates[0].norm
	bestCount := 0
	bestFirst := len(candidates)
	for norm, count := range counts {
		fi := firstIdx[norm]
		if count > bestCount || (count == bestCount && fi < bestFirst) {
			bestNorm = norm
			bestCount = count
			bestFirst = fi
		}
	}

	return candidates[firstIdx[bestNorm]].raw, nil
}

// normalizeAnswer collapses an LLM answer to a comparable canonical form:
// trims outer whitespace, case-folds, and collapses internal whitespace.
func normalizeAnswer(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

// selfConsistencyCandidate is one self-consistency sample's grouped result,
// for dev-mode inspection of every parallel sample rather than just the
// voted winner.
type selfConsistencyCandidate struct {
	Content string `json:"content"`
	Votes   int    `json:"votes"`
	Chosen  bool   `json:"chosen"`
}

// summarizeCandidates groups bodies by normalized content (same grouping
// selectByVote uses) and marks whichever group produced winner as chosen.
// Malformed bodies are skipped. Order follows first occurrence in bodies.
func summarizeCandidates(bodies [][]byte, winner []byte) []selfConsistencyCandidate {
	type group struct {
		content string
		votes   int
	}
	var order []string
	groups := map[string]*group{}
	for _, b := range bodies {
		content, ok := extractContent(b)
		if !ok {
			continue
		}
		norm := normalizeAnswer(content)
		g, exists := groups[norm]
		if !exists {
			g = &group{content: content}
			groups[norm] = g
			order = append(order, norm)
		}
		g.votes++
	}

	winnerNorm := ""
	if content, ok := extractContent(winner); ok {
		winnerNorm = normalizeAnswer(content)
	}

	summaries := make([]selfConsistencyCandidate, 0, len(order))
	for _, norm := range order {
		g := groups[norm]
		summaries = append(summaries, selfConsistencyCandidate{
			Content: g.content,
			Votes:   g.votes,
			Chosen:  norm == winnerNorm,
		})
	}
	return summaries
}

// attachCandidates adds a self_consistency_candidates field to a raw chat
// completion response body for dev-mode inspection. Returns raw unchanged
// (and candidates dropped) if candidates is empty or raw doesn't parse as
// JSON — never breaks a client for the sake of a debug field.
func attachCandidates(raw []byte, candidates []selfConsistencyCandidate) []byte {
	if len(candidates) == 0 {
		return raw
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return raw
	}
	obj["self_consistency_candidates"] = candidates
	out, err := json.Marshal(obj)
	if err != nil {
		return raw
	}
	return out
}
