// Package design turns RewardBench 2 rows into the frozen v1 request plan:
// four-response items, Williams-square orderings, and byte-exact bodies.
//
// Everything here is deterministic. Selection, response order and codes come
// from SHA-256 of a fixed seed and the item key, never from model outcomes.
package design

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math/rand/v2"
	"sort"
	"strings"

	"github.com/dorkitude/order-blind-decisions/internal/dataset"
	"github.com/dorkitude/order-blind-decisions/internal/jsono"
)

// Seed namespaces every deterministic draw in v1.
const Seed = "order-blind-decisions/v1"

// CanonicalModel is the model name in frozen bodies; providers swap it.
const CanonicalModel = "jev-1.13.0"

// K is the number of responses per item in v1.
const K = 4

// PlantedItems is the size of the planted-bias sensitivity arm.
const PlantedItems = 100

// Repeats is the number of byte-identical sends of every request.
const Repeats = 2

// Williams is a balanced Latin square for K=4: across the rows every
// response occupies every slot once and every ordered neighbour pair once.
var Williams = [K][K]int{{0, 1, 3, 2}, {1, 2, 0, 3}, {2, 3, 1, 0}, {3, 0, 2, 1}}

// Candidate is one response in an item.
type Candidate struct {
	Code    string `json:"code"`
	Text    string `json:"text"`
	Correct bool   `json:"correct"`
	Source  string `json:"source"` // e.g. "chosen[0]", "rejected[2]"
}

// Item is one RewardBench 2 prompt with its four selected responses in base
// order (a seeded shuffle, so the correct response has no fixed slot).
type Item struct {
	Key        string      `json:"key"` // subset slug + "/" + RewardBench 2 id
	ID         string      `json:"rb2_id"`
	Subset     string      `json:"subset"`
	Kind       string      `json:"kind"` // standard, tied or ref
	Prompt     string      `json:"prompt"`
	Candidates []Candidate `json:"candidates"`
}

// Request is one planned call. Bodies are rebuilt from the item and verified
// against BodySHA256 before sending.
type Request struct {
	ID            string   `json:"id"`
	Item          string   `json:"item"`
	Kind          string   `json:"kind"`
	Format        string   `json:"format"` // choice, packed or solo
	Arm           string   `json:"arm"`    // haystack, question or solo
	Ordering      int      `json:"ordering"`
	Repeat        int      `json:"repeat"`
	Planted       bool     `json:"planted,omitempty"`
	StateOrder    []string `json:"state_order"`
	QuestionOrder []string `json:"question_order"`
	BodySHA256    string   `json:"body_sha256"`
	BodyBytes     int      `json:"body_bytes"`
}

func rng(key, purpose string) *rand.Rand {
	h := sha256.Sum256([]byte(Seed + "|" + key + "|" + purpose))
	return rand.New(rand.NewPCG(binary.BigEndian.Uint64(h[:8]), binary.BigEndian.Uint64(h[8:16])))
}

// Rank orders keys by a seeded hash, for outcome-blind subsampling.
func Rank(key, purpose string) string {
	h := sha256.Sum256([]byte(Seed + "|" + purpose + "|" + key))
	return hex.EncodeToString(h[:])
}

func slug(s string) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(s)), " ", "-")
}

func kind(r dataset.Row) (string, error) {
	if r.Subset != "Ties" {
		return "standard", nil
	}
	switch {
	case strings.HasPrefix(r.ID, "tied:"):
		return "tied", nil
	case strings.HasPrefix(r.ID, "ref:"):
		return "ref", nil
	}
	return "", fmt.Errorf("Ties row %q is neither tied: nor ref:", r.ID)
}

func pick(r *rand.Rand, xs []string, n int, label string) ([]Candidate, error) {
	if len(xs) < n {
		return nil, fmt.Errorf("need %d %s responses, have %d", n, label, len(xs))
	}
	idx := r.Perm(len(xs))[:n]
	sort.Ints(idx)
	out := make([]Candidate, n)
	for i, j := range idx {
		out[i] = Candidate{Text: xs[j], Correct: label == "chosen", Source: fmt.Sprintf("%s[%d]", label, j)}
	}
	return out, nil
}

const codeFirst = "abcdefghijkmnpqrstuvwxyz"
const codeRest = "abcdefghijkmnpqrstuvwxyz23456789"

func codes(r *rand.Rand, n int) []string {
	seen := map[string]bool{}
	out := []string{}
	for len(out) < n {
		b := []byte{codeFirst[r.IntN(len(codeFirst))]}
		for i := 0; i < 3; i++ {
			b = append(b, codeRest[r.IntN(len(codeRest))])
		}
		if c := string(b); !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	return out
}

// BuildItems selects four responses per row:
//   - standard subsets: the chosen response and the three rejected ones;
//   - Ties tied prompts: two seeded correct and two seeded wrong responses;
//   - Ties ref prompts: the single correct response and three seeded wrong ones.
func BuildItems(rows []dataset.Row) ([]Item, error) {
	items := make([]Item, 0, len(rows))
	seen := map[string]bool{}
	for _, row := range rows {
		k, err := kind(row)
		if err != nil {
			return nil, err
		}
		key := slug(row.Subset) + "/" + row.ID
		if seen[key] {
			return nil, fmt.Errorf("duplicate item key %q", key)
		}
		seen[key] = true
		r := rng(key, "select")
		var cs []Candidate
		switch k {
		case "standard":
			if len(row.Chosen) != 1 || len(row.Rejected) != 3 {
				return nil, fmt.Errorf("%s: want 1 chosen + 3 rejected, have %d + %d", key, len(row.Chosen), len(row.Rejected))
			}
			cs, _ = pick(r, row.Chosen, 1, "chosen")
			rej, _ := pick(r, row.Rejected, 3, "rejected")
			cs = append(cs, rej...)
		case "tied":
			if cs, err = pick(r, row.Chosen, 2, "chosen"); err != nil {
				return nil, fmt.Errorf("%s: %w", key, err)
			}
			rej, err := pick(r, row.Rejected, 2, "rejected")
			if err != nil {
				return nil, fmt.Errorf("%s: %w", key, err)
			}
			cs = append(cs, rej...)
		case "ref":
			if len(row.Chosen) != 1 {
				return nil, fmt.Errorf("%s: ref prompt has %d correct responses, want 1", key, len(row.Chosen))
			}
			cs, _ = pick(r, row.Chosen, 1, "chosen")
			rej, err := pick(r, row.Rejected, 3, "rejected")
			if err != nil {
				return nil, fmt.Errorf("%s: %w", key, err)
			}
			cs = append(cs, rej...)
		}
		base := rng(key, "base-order").Perm(K)
		ordered := make([]Candidate, K)
		for i, j := range base {
			ordered[i] = cs[j]
		}
		for i, c := range codes(rng(key, "codes"), K) {
			ordered[i].Code = c
		}
		items = append(items, Item{Key: key, ID: row.ID, Subset: row.Subset, Kind: k, Prompt: row.Prompt, Candidates: ordered})
	}
	return items, nil
}

// Planted returns the keys of the planted-bias items: the first
// PlantedItems by seeded hash rank.
func Planted(items []Item) map[string]bool {
	keys := make([]string, len(items))
	for i, it := range items {
		keys[i] = it.Key
	}
	sort.Slice(keys, func(a, b int) bool { return Rank(keys[a], "planted") < Rank(keys[b], "planted") })
	out := map[string]bool{}
	for _, k := range keys[:min(PlantedItems, len(keys))] {
		out[k] = true
	}
	return out
}

// Pilot returns perKind seeded items for each subset and Ties kind.
func Pilot(items []Item, perKind int) map[string]bool {
	groups := map[string][]string{}
	for _, it := range items {
		g := it.Subset
		if it.Kind != "standard" {
			g = "Ties/" + it.Kind
		}
		groups[g] = append(groups[g], it.Key)
	}
	out := map[string]bool{}
	for _, keys := range groups {
		sort.Slice(keys, func(a, b int) bool { return Rank(keys[a], "pilot") < Rank(keys[b], "pilot") })
		for _, k := range keys[:min(perKind, len(keys))] {
			out[k] = true
		}
	}
	return out
}

func (it Item) codesIn(order [K]int) []string {
	out := make([]string, K)
	for i, j := range order {
		out[i] = it.Candidates[j].Code
	}
	return out
}

// Plan lists every request for one item, in a fixed order.
func (it Item) Plan(planted bool) ([]Request, error) {
	identity := [K]int{0, 1, 2, 3}
	var reqs []Request
	add := func(r Request) error {
		body, err := it.Body(r, CanonicalModel)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(body)
		r.BodySHA256, r.BodyBytes = hex.EncodeToString(sum[:]), len(body)
		reqs = append(reqs, r)
		return nil
	}
	for _, format := range []string{"choice", "packed"} {
		for _, arm := range []string{"haystack", "question"} {
			for o, row := range Williams {
				state, questions := it.codesIn(row), it.codesIn(row)
				if arm == "question" {
					state = it.codesIn(identity)
				}
				for rep := 1; rep <= Repeats; rep++ {
					if err := add(Request{
						ID:   fmt.Sprintf("%s|%s|%s|o%d|r%d", it.Key, format, arm, o, rep),
						Item: it.Key, Kind: it.Kind, Format: format, Arm: arm, Ordering: o, Repeat: rep,
						StateOrder: state, QuestionOrder: questions,
					}); err != nil {
						return nil, err
					}
				}
			}
		}
	}
	for c := range it.Candidates {
		code := it.Candidates[c].Code
		for rep := 1; rep <= Repeats; rep++ {
			if err := add(Request{
				ID:   fmt.Sprintf("%s|solo|c%d|r%d", it.Key, c, rep),
				Item: it.Key, Kind: it.Kind, Format: "solo", Arm: "solo", Ordering: c, Repeat: rep,
				StateOrder: []string{code}, QuestionOrder: []string{code},
			}); err != nil {
				return nil, err
			}
		}
	}
	if planted {
		for _, format := range []string{"choice", "packed"} {
			for o, row := range Williams {
				for rep := 1; rep <= Repeats; rep++ {
					if err := add(Request{
						ID:   fmt.Sprintf("%s|%s|planted|o%d|r%d", it.Key, format, o, rep),
						Item: it.Key, Kind: it.Kind, Format: format, Arm: "haystack", Ordering: o, Repeat: rep, Planted: true,
						StateOrder: it.codesIn(row), QuestionOrder: it.codesIn(row),
					}); err != nil {
						return nil, err
					}
				}
			}
		}
	}
	return reqs, nil
}

func (it Item) byCode(code string) (Candidate, error) {
	for _, c := range it.Candidates {
		if c.Code == code {
			return c, nil
		}
	}
	return Candidate{}, fmt.Errorf("%s: no candidate with code %q", it.Key, code)
}

func (it Item) ties() bool { return it.Subset == "Ties" }

func (it Item) blocks(order []string) (string, error) {
	parts := make([]string, 0, len(order))
	for _, code := range order {
		c, err := it.byCode(code)
		if err != nil {
			return "", err
		}
		parts = append(parts, "[The Start of Assistant "+code+"'s Answer]\n"+c.Text+"\n[The End of Assistant "+code+"'s Answer]")
	}
	return strings.Join(parts, "\n\n"), nil
}

func levels() []string {
	out := make([]string, 10)
	for i := range out {
		out[i] = fmt.Sprint(i + 1)
	}
	return out
}

// Body renders the exact request bytes for r with the given model name.
func (it Item) Body(r Request, model string) ([]byte, error) {
	var state string
	var questions jsono.Obj
	switch r.Format {
	case "choice":
		sys := choiceSystem
		if r.Planted {
			sys += " " + PlantedBias
		}
		b, err := it.blocks(r.StateOrder)
		if err != nil {
			return nil, err
		}
		state = sys + "\n\n[User Question]\n" + it.Prompt + "\n\n" + b
		criteria := jsono.Obj{}
		for _, code := range r.QuestionOrder {
			criteria = append(criteria, jsono.KV{K: code, V: "Assistant " + code + " is best."})
		}
		questions = jsono.Obj{{K: "evaluation", V: jsono.Obj{
			{K: "type", V: "choice"}, {K: "instructions", V: TypedInstructions}, {K: "criteria", V: criteria},
		}}}
	case "packed":
		head := packedHeader
		if it.ties() {
			head = packedHeaderTies
		}
		if r.Planted {
			head += "\n4- " + PlantedBias
		}
		b, err := it.blocks(r.StateOrder)
		if err != nil {
			return nil, err
		}
		state = head + "\n\n[Query]\n" + it.Prompt + "\n\n" + b + "\n\n[Your judgement]"
		for _, code := range r.QuestionOrder {
			questions = append(questions, jsono.KV{K: "rate_" + code, V: jsono.Obj{
				{K: "type", V: "score"},
				{K: "instructions", V: TypedInstructions + " Rate only Assistant " + code + "'s answer."},
				{K: "criteria", V: levels()},
			}})
		}
	case "solo":
		if r.Planted || len(r.StateOrder) != 1 {
			return nil, fmt.Errorf("%s: malformed solo request", r.ID)
		}
		c, err := it.byCode(r.StateOrder[0])
		if err != nil {
			return nil, err
		}
		t := ratingsSolo
		if it.ties() {
			t = ratingsSoloTies
		}
		// Single-pass replacement: response text is data, never template syntax.
		state = strings.NewReplacer("{prompt}", it.Prompt, "{completion}", c.Text).Replace(t)
		questions = jsono.Obj{{K: "evaluation", V: jsono.Obj{
			{K: "type", V: "score"}, {K: "instructions", V: TypedInstructions}, {K: "criteria", V: levels()},
		}}}
	default:
		return nil, fmt.Errorf("%s: unknown format %q", r.ID, r.Format)
	}
	return jsono.Marshal(jsono.Obj{{K: "model", V: model}, {K: "state", V: state}, {K: "questions", V: questions}})
}
