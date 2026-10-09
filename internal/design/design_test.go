package design

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dorkitude/order-blind-decisions/internal/dataset"
)

func rows() []dataset.Row {
	return []dataset.Row{
		{ID: "0", Subset: "Factuality", Prompt: "Who was Vitellius?", Chosen: []string{"good"}, Rejected: []string{"bad1", "bad2", "bad3"}},
		{ID: "0", Subset: "Precise IF", Prompt: "Use {completion} literally.", Chosen: []string{"ok {prompt}"}, Rejected: []string{"x", "y", "z"}},
		{ID: "tied:3", Subset: "Ties", Prompt: "Select a random day of the week.", Chosen: []string{"Monday", "Tuesday", "Wednesday"}, Rejected: []string{"Yesterday", "Tomorrow", "Midweek"}},
		{ID: "ref:3", Subset: "Ties", Prompt: "Day after the weekend?", Chosen: []string{"Monday"}, Rejected: []string{"Tuesday", "Wednesday", "Thursday", "Friday"}},
	}
}

func TestWilliamsIsBalanced(t *testing.T) {
	slot := [K][K]int{}
	pairs := map[[2]int]int{}
	for _, row := range Williams {
		for p, c := range row {
			slot[p][c]++
			if p > 0 {
				pairs[[2]int{row[p-1], c}]++
			}
		}
	}
	for p := range slot {
		for c := range slot[p] {
			if slot[p][c] != 1 {
				t.Fatalf("candidate %d appears %d times in slot %d", c, slot[p][c], p)
			}
		}
	}
	if len(pairs) != K*(K-1) {
		t.Fatalf("want %d distinct ordered neighbour pairs, got %d", K*(K-1), len(pairs))
	}
	for pr, n := range pairs {
		if n != 1 {
			t.Fatalf("neighbour pair %v appears %d times", pr, n)
		}
	}
}

func TestItemsAreDeterministicAndFollowSelectionRules(t *testing.T) {
	a, err := BuildItems(rows())
	if err != nil {
		t.Fatal(err)
	}
	b, _ := BuildItems(rows())
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	if string(ja) != string(jb) {
		t.Fatal("BuildItems is not deterministic")
	}
	want := map[string]int{"factuality/0": 1, "precise-if/0": 1, "ties/tied:3": 2, "ties/ref:3": 1}
	for _, it := range a {
		correct := 0
		codes := map[string]bool{}
		for _, c := range it.Candidates {
			if c.Correct {
				correct++
			}
			codes[c.Code] = true
		}
		if correct != want[it.Key] || len(it.Candidates) != K || len(codes) != K {
			t.Fatalf("%s: %d correct, %d candidates, %d codes", it.Key, correct, len(it.Candidates), len(codes))
		}
	}
	if _, err := BuildItems(append(rows(), rows()[0])); err == nil {
		t.Fatal("duplicate subset/id key was accepted")
	}
}

func TestPlanShapeAndOrderReachesTheBytes(t *testing.T) {
	items, _ := BuildItems(rows())
	it := items[0]
	reqs, err := it.Plan(true)
	if err != nil {
		t.Fatal(err)
	}
	// choice+packed × haystack+question × 4 orderings × 2 repeats, solo 4 × 2, planted 2 × 4 × 2.
	if want := 2*2*K*Repeats + K*Repeats + 2*K*Repeats; len(reqs) != want {
		t.Fatalf("want %d requests, got %d", want, len(reqs))
	}
	ids := map[string]bool{}
	bySHA := map[string]int{}
	for _, r := range reqs {
		if ids[r.ID] {
			t.Fatalf("duplicate request id %s", r.ID)
		}
		ids[r.ID] = true
		bySHA[r.BodySHA256]++
		body, _ := it.Body(r, CanonicalModel)
		s := string(body)
		// The state lists responses in StateOrder; questions follow QuestionOrder.
		last := -1
		for _, code := range r.StateOrder {
			i := strings.Index(s, "[The Start of Assistant "+code)
			if r.Format == "solo" {
				break
			}
			if i <= last {
				t.Fatalf("%s: state order not preserved for %s", r.ID, code)
			}
			last = i
		}
		qs := s[strings.Index(s, `"questions"`):]
		last = -1
		for _, code := range r.QuestionOrder {
			i := strings.Index(qs, `"`+code+`"`)
			if r.Format == "packed" {
				i = strings.Index(qs, `"rate_`+code+`"`)
			}
			if r.Format == "solo" {
				break
			}
			if i <= last {
				t.Fatalf("%s: question order not preserved for %s", r.ID, code)
			}
			last = i
		}
		if r.Planted != strings.Contains(s, PlantedBias) {
			t.Fatalf("%s: planted sentence mismatch", r.ID)
		}
		if strings.Contains(s, "Avoid any position biases") {
			t.Fatalf("%s: anti-bias sentence was not removed", r.ID)
		}
	}
	// Repeats are byte-identical; distinct orderings are not.
	for sha, n := range bySHA {
		if n%Repeats != 0 {
			t.Fatalf("body %s sent %d times, want a multiple of %d", sha, n, Repeats)
		}
	}
}

func TestTemplateTextIsData(t *testing.T) {
	items, _ := BuildItems(rows())
	for _, it := range items {
		if it.Key != "precise-if/0" {
			continue
		}
		reqs, _ := it.Plan(false)
		for _, r := range reqs {
			if r.Format != "solo" {
				continue
			}
			body, _ := it.Body(r, CanonicalModel)
			var b struct{ State string }
			json.Unmarshal(body, &b)
			if !strings.Contains(b.State, "Use {completion} literally.") {
				t.Fatalf("prompt braces were substituted: %q", b.State)
			}
		}
	}
}

func TestSubsamplesAreStable(t *testing.T) {
	items, _ := BuildItems(rows())
	if p := Pilot(items, 1); len(p) != 4 {
		t.Fatalf("pilot with 1 per group: want 4 items, got %d", len(p))
	}
	if p := Planted(items); len(p) != len(items) {
		t.Fatalf("planted set should cap at the item count, got %d", len(p))
	}
}
