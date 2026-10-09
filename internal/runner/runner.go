// Package runner sends planned requests to providers, writes a receipt for
// every attempt, and enforces the project-wide budget.
package runner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dorkitude/order-blind-decisions/internal/provider"
	"github.com/dorkitude/order-blind-decisions/internal/runlog"
)

// Job is one planned request. Body renders the bytes for a model name;
// Canonical is the SHA-256 the body must have with CanonicalModel.
type Job struct {
	ID             string
	Body           func(model string) ([]byte, error)
	CanonicalModel string
	Canonical      string
}

// Options configure a run.
type Options struct {
	Root            string         // runs/ directory
	Run             string         // run name, e.g. "probe", "pilot", "main"
	Workers         int            // concurrent requests per provider
	WorkersBy       map[string]int // per-provider override of Workers
	BudgetUSD       float64
	MaxAttempts     int
	MaxConsecFail   int // stop a provider after this many requests fail in a row
	Log             io.Writer
	ProgressEvery   int
	BytesPerToken   float64 // for cost reservations when usage is unknown
	ReserveMultiple float64 // safety factor on reservations
}

// ErrBudget reports that the next call would exceed the budget.
var ErrBudget = errors.New("budget cap reached")

type ledger struct {
	mu       sync.Mutex
	spent    float64
	reserved float64
	cap      float64
}

func (l *ledger) reserve(usd float64) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.spent+l.reserved+usd > l.cap {
		return false
	}
	l.reserved += usd
	return true
}

func (l *ledger) settle(reserved, actual float64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.reserved -= reserved
	l.spent += actual
}

// Summary reports one provider's run.
type Summary struct {
	Provider             string
	Planned, Skipped, OK int
	Failed, Attempts     int
	CostUSD              float64
	StoppedBy            string
}

func sha(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }

// Run sends jobs to every provider. Providers proceed in parallel, each
// walking jobs in the same order, so a stop leaves comparable coverage.
func Run(ctx context.Context, o Options, providers []provider.Provider, jobs []Job) ([]Summary, error) {
	if o.Workers < 1 {
		o.Workers = 1
	}
	if o.MaxAttempts < 1 {
		o.MaxAttempts = 4
	}
	if o.MaxConsecFail < 1 {
		o.MaxConsecFail = 20
	}
	if o.BytesPerToken <= 0 {
		o.BytesPerToken = 3
	}
	if o.ReserveMultiple <= 0 {
		o.ReserveMultiple = 1.5
	}
	// Verify every body before any call.
	for _, j := range jobs {
		b, err := j.Body(j.CanonicalModel)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", j.ID, err)
		}
		if got := sha(b); got != j.Canonical {
			return nil, fmt.Errorf("%s: body sha256 %s does not match the frozen plan %s", j.ID, got, j.Canonical)
		}
	}
	spent, err := runlog.Spent(o.Root)
	if err != nil {
		return nil, err
	}
	l := &ledger{spent: spent, cap: o.BudgetUSD}
	fmt.Fprintf(o.Log, "project spend so far $%.4f of $%.2f cap\n", spent, o.BudgetUSD)

	var wg sync.WaitGroup
	sums := make([]Summary, len(providers))
	errs := make([]error, len(providers))
	for i, p := range providers {
		wg.Add(1)
		go func(i int, p provider.Provider) {
			defer wg.Done()
			sums[i], errs[i] = runProvider(ctx, o, l, p, jobs)
		}(i, p)
	}
	wg.Wait()
	return sums, errors.Join(errs...)
}

func retryable(status int) bool {
	return status == 0 || status == http.StatusTooManyRequests || status == http.StatusRequestTimeout || status >= 500
}

func runProvider(ctx context.Context, o Options, l *ledger, p provider.Provider, jobs []Job) (Summary, error) {
	s := Summary{Provider: p.Name(), Planned: len(jobs)}
	done, err := runlog.Done(o.Root, o.Run, p.Name())
	if err != nil {
		return s, err
	}
	w, err := runlog.Open(o.Root, o.Run, p.Name())
	if err != nil {
		return s, err
	}
	defer w.Close()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var mu sync.Mutex
	var consec, finished int64
	stop := func(reason string) {
		mu.Lock()
		if s.StoppedBy == "" {
			s.StoppedBy = reason
		}
		mu.Unlock()
		cancel()
	}
	workers := o.Workers
	if n, ok := o.WorkersBy[p.Name()]; ok && n > 0 {
		workers = n
	}
	fmt.Fprintf(o.Log, "%s: %d workers\n", p.Name(), workers)
	queue := make(chan Job)
	var wg sync.WaitGroup
	for k := 0; k < workers; k++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range queue {
				ok, attempts, cost, err := send(ctx, o, l, p, w, j)
				mu.Lock()
				s.Attempts += attempts
				s.CostUSD += cost
				if ok {
					s.OK++
				} else if !errors.Is(err, context.Canceled) {
					s.Failed++
				}
				mu.Unlock()
				switch {
				case errors.Is(err, ErrBudget):
					stop("budget")
				case ok:
					atomic.StoreInt64(&consec, 0)
				case errors.Is(err, context.Canceled):
				default:
					if atomic.AddInt64(&consec, 1) >= int64(o.MaxConsecFail) {
						stop(fmt.Sprintf("%d consecutive failures (last: %v)", o.MaxConsecFail, err))
					}
				}
				if n := atomic.AddInt64(&finished, 1); o.ProgressEvery > 0 && n%int64(o.ProgressEvery) == 0 {
					mu.Lock()
					fmt.Fprintf(o.Log, "%s: %d/%d sent this run, %d ok, %d failed, $%.4f\n", p.Name(), n, len(jobs)-s.Skipped, s.OK, s.Failed, s.CostUSD)
					mu.Unlock()
				}
			}
		}()
	}
feed:
	for _, j := range jobs {
		if done[j.ID] {
			s.Skipped++
			continue
		}
		select {
		case queue <- j:
		case <-ctx.Done():
			break feed
		}
	}
	close(queue)
	wg.Wait()
	return s, nil
}

func send(ctx context.Context, o Options, l *ledger, p provider.Provider, w *runlog.Writer, j Job) (ok bool, attempts int, cost float64, err error) {
	body, err := j.Body(p.BodyModel())
	if err != nil {
		return false, 0, 0, err
	}
	estTokens := float64(len(body)) / o.BytesPerToken
	reserve := estTokens * p.USDPerMTokInput() / 1e6 * o.ReserveMultiple
	for attempt := 1; attempt <= o.MaxAttempts; attempt++ {
		if ctx.Err() != nil {
			return false, attempts, cost, ctx.Err()
		}
		if !l.reserve(reserve) {
			return false, attempts, cost, ErrBudget
		}
		started := time.Now().UTC()
		res, callErr := p.Do(ctx, body)
		attempts++
		rc := runlog.Receipt{
			Run: o.Run, RequestID: j.ID, Provider: p.Name(), Attempt: attempt,
			StartedUTC: started.Format(time.RFC3339Nano), Seconds: res.Seconds, Status: res.Status,
			BodySHA256: j.Canonical, SentSHA256: res.SentSHA256, ResponseModel: res.ResponseModel,
			InputTokens: res.InputTokens, OutputTokens: res.OutputTokens, UsageReported: res.UsageReported,
			ParseError: res.ParseError,
		}
		if callErr != nil {
			rc.Error = callErr.Error()
		}
		rc.SetBodies(res.Raw, res.Normalized)
		// Price reported input tokens; without usage, any response that came
		// back is charged at the estimate so spend is never understated.
		switch {
		case res.UsageReported:
			rc.CostUSD = float64(res.InputTokens) * p.USDPerMTokInput() / 1e6
		case res.Status != 0:
			rc.CostUSD, rc.CostEstimated = estTokens*p.USDPerMTokInput()/1e6, true
		}
		rc.OK = callErr == nil && res.Status == http.StatusOK && res.Normalized != nil
		l.settle(reserve, rc.CostUSD)
		cost += rc.CostUSD
		if werr := w.Write(rc); werr != nil {
			return false, attempts, cost, werr
		}
		if rc.OK {
			return true, attempts, cost, nil
		}
		if callErr != nil && ctx.Err() != nil {
			return false, attempts, cost, ctx.Err()
		}
		if res.Status != 0 && !retryable(res.Status) && res.ParseError == "" {
			return false, attempts, cost, fmt.Errorf("%s: HTTP %d", j.ID, res.Status)
		}
		wait := time.Duration(1<<attempt) * time.Second
		if res.RetryAfter > 0 {
			wait = time.Duration(res.RetryAfter) * time.Second
		}
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return false, attempts, cost, ctx.Err()
		}
	}
	return false, attempts, cost, fmt.Errorf("%s: no success after %d attempts", j.ID, o.MaxAttempts)
}
