// Package runlog writes and reads the append-only JSONL receipts in runs/.
//
// Layout: runs/<run>/<provider>/receipts-NNNN.jsonl, one line per HTTP
// attempt. Files rotate before MaxShardBytes so none approaches GitHub's
// 100 MB limit. Receipts are never rewritten; db/ is rebuilt from them.
package runlog

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// MaxShardBytes is the rotation threshold for one receipts file.
const MaxShardBytes = 45 << 20

// Receipt is one HTTP attempt.
type Receipt struct {
	Run           string          `json:"run"`
	RequestID     string          `json:"request_id"`
	Provider      string          `json:"provider"`
	Attempt       int             `json:"attempt"`
	StartedUTC    string          `json:"started_utc"`
	Seconds       float64         `json:"seconds"`
	Status        int             `json:"status"`
	OK            bool            `json:"ok"`
	BodySHA256    string          `json:"body_sha256"`
	SentSHA256    string          `json:"sent_sha256,omitempty"`
	ResponseModel string          `json:"response_model,omitempty"`
	InputTokens   int             `json:"input_tokens"`
	OutputTokens  int             `json:"output_tokens"`
	UsageReported bool            `json:"usage_reported"`
	CostUSD       float64         `json:"cost_usd"`
	CostEstimated bool            `json:"cost_estimated,omitempty"`
	Error         string          `json:"error,omitempty"`
	ParseError    string          `json:"parse_error,omitempty"`
	Raw           json.RawMessage `json:"raw,omitempty"`
	RawText       string          `json:"raw_text,omitempty"` // when the body is not JSON
	Normalized    json.RawMessage `json:"normalized,omitempty"`
}

// SetBodies stores the raw and Jev-shaped bodies, omitting the normalized
// copy when it equals the raw body (Jev).
func (r *Receipt) SetBodies(raw, normalized []byte) {
	if len(raw) > 0 {
		if json.Valid(raw) {
			r.Raw = compact(raw)
		} else {
			r.RawText = string(raw)
		}
	}
	if len(normalized) > 0 && string(compact(normalized)) != string(r.Raw) {
		r.Normalized = compact(normalized)
	}
}

func compact(b []byte) json.RawMessage {
	var out bytes.Buffer
	if err := json.Compact(&out, b); err != nil {
		return json.RawMessage(b)
	}
	return json.RawMessage(out.Bytes())
}

// Writer appends receipts for one run and provider. It is safe for
// concurrent use.
type Writer struct {
	mu    sync.Mutex
	dir   string
	shard int
	f     *os.File
	size  int64
}

// Open prepares runs/<run>/<provider>/ for appending.
func Open(root, run, provider string) (*Writer, error) {
	dir := filepath.Join(root, run, provider)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	w := &Writer{dir: dir}
	files, _ := filepath.Glob(filepath.Join(dir, "receipts-*.jsonl"))
	sort.Strings(files)
	w.shard = len(files)
	if w.shard == 0 {
		w.shard = 1
	}
	return w, w.openShard()
}

func (w *Writer) openShard() error {
	p := filepath.Join(w.dir, fmt.Sprintf("receipts-%04d.jsonl", w.shard))
	f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	w.f, w.size = f, st.Size()
	return nil
}

// Write appends one receipt and flushes it to the file.
func (w *Writer) Write(r Receipt) error {
	line, err := json.Marshal(r)
	if err != nil {
		return err
	}
	line = append(line, '\n')
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.size > 0 && w.size+int64(len(line)) > MaxShardBytes {
		if err := w.f.Close(); err != nil {
			return err
		}
		w.shard++
		if err := w.openShard(); err != nil {
			return err
		}
	}
	n, err := w.f.Write(line)
	w.size += int64(n)
	return err
}

// Close closes the current shard.
func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.f.Close()
}

// Walk calls fn for every receipt under root, in file order.
func Walk(root string, fn func(path string, r Receipt) error) error {
	var files []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(p, ".jsonl") && strings.HasPrefix(d.Name(), "receipts-") {
			files = append(files, p)
		}
		return nil
	})
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	sort.Strings(files)
	for _, p := range files {
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		s := bufio.NewScanner(f)
		s.Buffer(make([]byte, 1<<20), 64<<20)
		line := 0
		for s.Scan() {
			line++
			var r Receipt
			if err := json.Unmarshal(s.Bytes(), &r); err != nil {
				f.Close()
				return fmt.Errorf("%s:%d: %w", p, line, err)
			}
			if err := fn(p, r); err != nil {
				f.Close()
				return err
			}
		}
		f.Close()
		if err := s.Err(); err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
	}
	return nil
}

// Spent totals the cost of every receipt under root, across all runs.
func Spent(root string) (float64, error) {
	total := 0.0
	err := Walk(root, func(_ string, r Receipt) error { total += r.CostUSD; return nil })
	return total, err
}

// Done returns the request IDs with a successful receipt for one run and
// provider.
func Done(root, run, provider string) (map[string]bool, error) {
	done := map[string]bool{}
	err := Walk(filepath.Join(root, run, provider), func(_ string, r Receipt) error {
		if r.OK {
			done[r.RequestID] = true
		}
		return nil
	})
	return done, err
}
