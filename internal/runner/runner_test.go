package runner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/dorkitude/order-blind-decisions/internal/provider"
	"github.com/dorkitude/order-blind-decisions/internal/runlog"
)

type fake struct {
	calls   atomic.Int64
	failFor map[string]int // request body -> failures before success
	seen    map[string]*atomic.Int64
}

func (f *fake) Name() string             { return "fake" }
func (f *fake) BodyModel() string        { return "fake-model" }
func (f *fake) USDPerMTokInput() float64 { return 1000 } // $0.001 per token
func (f *fake) Do(_ context.Context, body []byte) (provider.Result, error) {
	f.calls.Add(1)
	n := f.seen[string(body)].Add(1)
	if int(n) <= f.failFor[string(body)] {
		return provider.Result{Status: http.StatusTooManyRequests, Raw: []byte(`{"error":"slow down"}`)}, nil
	}
	norm := []byte(`{"model":"fake-model","answers":{"evaluation":{"type":"choice","choice":"a"}},"usage":{"input_tokens":10,"output_tokens":0}}`)
	return provider.Result{Status: http.StatusOK, Raw: norm, Normalized: norm, InputTokens: 10, UsageReported: true}, nil
}

func jobs(n int) ([]Job, *fake) {
	f := &fake{failFor: map[string]int{}, seen: map[string]*atomic.Int64{}}
	var js []Job
	for i := 0; i < n; i++ {
		id := string(rune('a' + i))
		body := func(model string) ([]byte, error) { return []byte(model + "/" + id), nil }
		canon, _ := body("canon")
		s := sha256.Sum256(canon)
		send, _ := body(f.BodyModel())
		f.seen[string(send)] = &atomic.Int64{}
		js = append(js, Job{ID: id, Body: body, CanonicalModel: "canon", Canonical: hex.EncodeToString(s[:])})
	}
	return js, f
}

func opts(root string, budget float64) Options {
	return Options{Root: root, Run: "test", Workers: 2, BudgetUSD: budget, MaxAttempts: 3, Log: io.Discard, BytesPerToken: 1, ReserveMultiple: 1}
}

func TestRetryResumeAndBudget(t *testing.T) {
	root := t.TempDir()
	js, f := jobs(5)
	f.failFor["fake-model/a"] = 1 // one 429, then success
	sums, err := Run(context.Background(), opts(root, 1), []provider.Provider{f}, js)
	if err != nil {
		t.Fatal(err)
	}
	if s := sums[0]; s.OK != 5 || s.Attempts != 6 {
		t.Fatalf("want 5 ok in 6 attempts, got %+v", s)
	}
	// Resume: nothing left to send.
	before := f.calls.Load()
	sums, _ = Run(context.Background(), opts(root, 1), []provider.Provider{f}, js)
	if f.calls.Load() != before || sums[0].Skipped != 5 {
		t.Fatalf("resume resent requests: %+v", sums[0])
	}
	spent, _ := runlog.Spent(root)
	// 5 successes × 10 tokens × $0.001, plus the 429 charged at its estimate (12 bytes).
	if want := 0.05 + 0.012; spent < want-1e-9 || spent > want+1e-9 {
		t.Fatalf("spent %v, want %v", spent, want)
	}
	// A cap below the spend already recorded stops before any call.
	js2, f2 := jobs(3)
	for i := range js2 {
		js2[i].ID = "new-" + js2[i].ID
	}
	sums, _ = Run(context.Background(), opts(root, spent), []provider.Provider{f2}, js2)
	if f2.calls.Load() != 0 || sums[0].StoppedBy != "budget" {
		t.Fatalf("budget not enforced: calls=%d %+v", f2.calls.Load(), sums[0])
	}
}

func TestTamperedBodyIsRejected(t *testing.T) {
	js, f := jobs(1)
	js[0].Canonical = "0000"
	if _, err := Run(context.Background(), opts(t.TempDir(), 1), []provider.Provider{f}, js); err == nil || f.calls.Load() != 0 {
		t.Fatal("a body that does not match the frozen hash was sent")
	}
}
