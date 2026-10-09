// Package frozen writes and loads the frozen v1 plan in frozen/v1/:
// items.jsonl (selected responses and codes), requests.jsonl (every planned
// call with its body hash) and SHA256SUMS over both.
package frozen

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/dorkitude/order-blind-decisions/internal/design"
	"github.com/dorkitude/order-blind-decisions/internal/jsono"
	"github.com/dorkitude/order-blind-decisions/internal/runner"
)

var files = []string{"items.jsonl", "requests.jsonl"}

// Plan is a loaded frozen plan.
type Plan struct {
	Items    map[string]design.Item
	Order    []string // item keys in plan order
	Requests []design.Request
}

func writeJSONL[T any](path string, xs []T) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	w := bufio.NewWriter(f)
	for _, x := range xs {
		b, err := jsono.Marshal(x)
		if err != nil {
			f.Close()
			return err
		}
		w.Write(b)
		w.WriteByte('\n')
	}
	if err := w.Flush(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func fileSHA(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Write stores items and requests and their checksums in dir.
func Write(dir string, items []design.Item, reqs []design.Request) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := writeJSONL(filepath.Join(dir, files[0]), items); err != nil {
		return err
	}
	if err := writeJSONL(filepath.Join(dir, files[1]), reqs); err != nil {
		return err
	}
	var sums strings.Builder
	for _, f := range files {
		s, err := fileSHA(filepath.Join(dir, f))
		if err != nil {
			return err
		}
		fmt.Fprintf(&sums, "%s  %s\n", s, f)
	}
	return os.WriteFile(filepath.Join(dir, "SHA256SUMS"), []byte(sums.String()), 0o644)
}

func readJSONL[T any](path string) ([]T, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 1<<20), 64<<20)
	var out []T
	for s.Scan() {
		var x T
		if err := json.Unmarshal(s.Bytes(), &x); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		out = append(out, x)
	}
	return out, s.Err()
}

// Load verifies SHA256SUMS and reads the plan.
func Load(dir string) (Plan, error) {
	sums, err := os.ReadFile(filepath.Join(dir, "SHA256SUMS"))
	if err != nil {
		return Plan{}, err
	}
	for _, line := range strings.Split(strings.TrimSpace(string(sums)), "\n") {
		want, name, ok := strings.Cut(line, "  ")
		if !ok {
			return Plan{}, fmt.Errorf("malformed SHA256SUMS line %q", line)
		}
		got, err := fileSHA(filepath.Join(dir, name))
		if err != nil {
			return Plan{}, err
		}
		if got != want {
			return Plan{}, fmt.Errorf("%s changed since the plan was frozen (sha256 %s, want %s)", name, got, want)
		}
	}
	items, err := readJSONL[design.Item](filepath.Join(dir, files[0]))
	if err != nil {
		return Plan{}, err
	}
	reqs, err := readJSONL[design.Request](filepath.Join(dir, files[1]))
	if err != nil {
		return Plan{}, err
	}
	p := Plan{Items: map[string]design.Item{}, Requests: reqs}
	for _, it := range items {
		p.Items[it.Key] = it
		p.Order = append(p.Order, it.Key)
	}
	return p, nil
}

// Jobs converts requests to runner jobs, keeping plan order.
func (p Plan) Jobs(keep func(design.Request) bool) ([]runner.Job, error) {
	var out []runner.Job
	for _, r := range p.Requests {
		if keep != nil && !keep(r) {
			continue
		}
		it, ok := p.Items[r.Item]
		if !ok {
			return nil, fmt.Errorf("%s: unknown item %q", r.ID, r.Item)
		}
		r := r
		out = append(out, runner.Job{
			ID:             r.ID,
			Body:           func(model string) ([]byte, error) { return it.Body(r, model) },
			CanonicalModel: design.CanonicalModel,
			Canonical:      r.BodySHA256,
		})
	}
	return out, nil
}
