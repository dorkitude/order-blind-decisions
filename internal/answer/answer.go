// Package answer reads Jev-shaped answers from normalized responses.
package answer

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// Answer is one typed answer.
type Answer struct {
	Type          string             `json:"type"`
	Choice        string             `json:"choice,omitempty"`
	Score         *float64           `json:"score,omitempty"` // 0-based expected level
	Probabilities map[string]float64 `json:"probabilities,omitempty"`
}

// Refused reports a provider refusal.
func (a Answer) Refused() bool { return a.Type == "refusal" }

// Rating converts a 0-based expected level on the 1–10 scale to the scale.
func (a Answer) Rating() (float64, bool) {
	if a.Score == nil {
		return 0, false
	}
	return *a.Score + 1, true
}

// ModalRating is the most probable level, on the 1–10 scale.
func (a Answer) ModalRating() (int, bool) {
	best, idx := -1.0, -1
	for k, p := range a.Probabilities {
		i, err := strconv.Atoi(k)
		if err != nil {
			return 0, false
		}
		if p > best || (p == best && i < idx) {
			best, idx = p, i
		}
	}
	return idx + 1, idx >= 0
}

// Parse returns the answers of a normalized response, keyed by question.
func Parse(normalized []byte) (map[string]Answer, error) {
	var b struct {
		Answers map[string]Answer `json:"answers"`
	}
	if err := json.Unmarshal(normalized, &b); err != nil {
		return nil, err
	}
	if len(b.Answers) == 0 {
		return nil, fmt.Errorf("no answers")
	}
	return b.Answers, nil
}
