// Package probe finds how much request state each provider actually reads.
//
// A needle sentence ("the access code for this archive is NNNN") is placed at
// the start or the end of neutral filler text of growing length, and the
// model is asked for the code. If a provider silently truncates long state,
// it keeps finding the start needle but loses the end needle once the text
// passes its limit. Truncation would mimic primacy, so this gates the study.
package probe

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/dorkitude/order-blind-decisions/internal/jsono"
	"github.com/dorkitude/order-blind-decisions/internal/runner"
)

// Lengths are target sizes in estimated tokens (about 4 characters each).
// The largest v1 request is about 5.3k tokens.
var Lengths = []int{500, 1000, 2000, 3000, 4000, 6000, 8000}

// Positions of the needle.
var Positions = []string{"start", "end"}

// Options are the candidate codes; Variants pick which one is the needle.
var Options = []string{"4417", "8302", "1956", "6620"}

// Variants are the needle codes; using two guards against a lucky guess.
var Variants = []string{"4417", "6620"}

// Case is one probe request.
type Case struct {
	ID       string
	Length   int
	Position string
	Needle   string
}

// Cases lists every probe request.
func Cases() []Case {
	var out []Case
	for _, n := range Lengths {
		for _, pos := range Positions {
			for _, v := range Variants {
				out = append(out, Case{ID: fmt.Sprintf("probe|len%05d|%s|code%s", n, pos, v), Length: n, Position: pos, Needle: v})
			}
		}
	}
	return out
}

var colors = []string{"red", "blue", "green", "grey", "white", "amber"}

func filler(i int) string {
	return fmt.Sprintf("Ledger line %04d: crate %d of %s cloth arrived at dock %d on schedule and was logged without remarks.", i, (i*37)%911, colors[i%len(colors)], i%9+1)
}

// State renders the probe text for a case.
func (c Case) State() string {
	needle := "Important: the access code for this archive is " + c.Needle + "."
	var lines []string
	chars := 0
	for i := 1; chars < c.Length*4; i++ {
		l := filler(i)
		lines = append(lines, l)
		chars += len(l) + 1
	}
	if c.Position == "start" {
		lines = append([]string{lines[0], needle}, lines[1:]...)
	} else {
		lines = append(lines[:len(lines)-1], needle, lines[len(lines)-1])
	}
	return strings.Join(lines, "\n")
}

// Body renders the request for a model name.
func (c Case) Body(model string) ([]byte, error) {
	criteria := jsono.Obj{}
	for _, o := range Options {
		criteria = append(criteria, jsono.KV{K: o, V: "The access code is " + o + "."})
	}
	return jsono.Marshal(jsono.Obj{
		{K: "model", V: model},
		{K: "state", V: c.State()},
		{K: "questions", V: jsono.Obj{{K: "evaluation", V: jsono.Obj{
			{K: "type", V: "choice"},
			{K: "instructions", V: "What is the access code for this archive, as stated in the text? Return that code."},
			{K: "criteria", V: criteria},
		}}}},
	})
}

// Jobs converts the cases to runner jobs.
func Jobs(canonicalModel string) ([]runner.Job, error) {
	var out []runner.Job
	for _, c := range Cases() {
		c := c
		b, err := c.Body(canonicalModel)
		if err != nil {
			return nil, err
		}
		s := sha256.Sum256(b)
		out = append(out, runner.Job{ID: c.ID, Body: c.Body, CanonicalModel: canonicalModel, Canonical: hex.EncodeToString(s[:])})
	}
	return out, nil
}
