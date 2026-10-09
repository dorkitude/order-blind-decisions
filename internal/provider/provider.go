// Package provider sends Jev-shaped request bodies to the three decision
// models and returns raw and Jev-shaped responses.
//
//   - jev: Jev's SystemOne API through the exe.dev "typesafe" integration,
//     which injects the key at the network edge.
//   - decisions: OpenAI's Decisions API through the exe.dev "openai"
//     integration; the public decision-model-testing adapter translates the
//     body and normalizes the answer back to Jev's shape.
//   - clef-flash: Cloudflare Workers AI, which accepts Jev's body directly and
//     wraps the answer in {"result": …, "success": …}. Needs
//     CLOUDFLARE_ACCOUNT_ID and CLOUDFLARE_API_TOKEN in the environment.
package provider

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"os"
	"strconv"
	"time"

	"github.com/dorkitude/decision-model-testing/experiments/openai-decisions-adapter/decisions"
)

// Result is one HTTP attempt.
type Result struct {
	Status        int
	Raw           []byte // provider response body, unmodified
	Normalized    []byte // Jev-shaped {model, answers, usage}; nil on failure
	ParseError    string
	Seconds       float64 // HTTP round trip
	RetryAfter    int     // seconds, from a Retry-After header
	SentSHA256    string  // hash of the exact bytes sent
	ResponseModel string
	InputTokens   int
	OutputTokens  int
	UsageReported bool
}

// Provider is one model route.
type Provider interface {
	Name() string
	// BodyModel is the model name to put in the Jev-shaped body.
	BodyModel() string
	// USDPerMTokInput is the list price used for accounting estimates.
	USDPerMTokInput() float64
	Do(ctx context.Context, body []byte) (Result, error)
}

// Names lists the providers in study order; Jev is the reference.
var Names = []string{"jev", "decisions", "clef-flash"}

// New returns the named provider.
func New(name string) (Provider, error) {
	switch name {
	case "jev":
		return jev{url: "https://typesafe.int.exe.xyz/v1/systemone", model: "jev-1.13.0"}, nil
	case "decisions":
		return openai{c: decisions.Client{Endpoint: "https://openai.int.exe.xyz/v1/decisions", Model: decisions.DefaultModel}}, nil
	case "clef-flash":
		acct, tok := os.Getenv("CLOUDFLARE_ACCOUNT_ID"), os.Getenv("CLOUDFLARE_API_TOKEN")
		if acct == "" || tok == "" {
			return nil, fmt.Errorf("clef-flash needs CLOUDFLARE_ACCOUNT_ID and CLOUDFLARE_API_TOKEN")
		}
		return clef{url: "https://api.cloudflare.com/client/v4/accounts/" + acct + "/ai/run/@cf/cloudflare/clef-flash", token: tok}, nil
	}
	return nil, fmt.Errorf("unknown provider %q (want one of %v)", name, Names)
}

func sha(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }

var client = &http.Client{Timeout: 2 * time.Minute}

func post(ctx context.Context, url, token string, body []byte) (Result, error) {
	r := Result{SentSHA256: sha(body)}
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return r, err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	t0 := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		r.Seconds = time.Since(t0).Seconds()
		// Drop the URL from the error: Cloudflare's contains the account ID,
		// and receipts are published.
		var ue *neturl.Error
		if errors.As(err, &ue) {
			err = fmt.Errorf("%s request failed: %w", ue.Op, ue.Err)
		}
		return r, err
	}
	defer resp.Body.Close()
	r.Raw, err = io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	r.Seconds = time.Since(t0).Seconds()
	r.Status = resp.StatusCode
	if n, e := strconv.Atoi(resp.Header.Get("Retry-After")); e == nil {
		r.RetryAfter = n
	}
	return r, err
}

// fill validates a Jev-shaped body and copies its model and usage into r.
func fill(r *Result, normalized []byte) {
	var b struct {
		Model   string                     `json:"model"`
		Answers map[string]json.RawMessage `json:"answers"`
		Usage   *struct {
			In  int `json:"input_tokens"`
			Out int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(normalized, &b); err != nil {
		r.ParseError = err.Error()
		return
	}
	if len(b.Answers) == 0 {
		r.ParseError = "response has no answers"
		return
	}
	r.Normalized, r.ResponseModel = normalized, b.Model
	if b.Usage != nil {
		r.InputTokens, r.OutputTokens, r.UsageReported = b.Usage.In, b.Usage.Out, true
	}
}

type jev struct{ url, model string }

func (p jev) Name() string             { return "jev" }
func (p jev) BodyModel() string        { return p.model }
func (p jev) USDPerMTokInput() float64 { return 0.042 }
func (p jev) Do(ctx context.Context, body []byte) (Result, error) {
	r, err := post(ctx, p.url, "", body)
	if err == nil && r.Status == http.StatusOK {
		fill(&r, r.Raw)
	}
	return r, err
}

type clef struct{ url, token string }

func (p clef) Name() string             { return "clef-flash" }
func (p clef) BodyModel() string        { return "clef-flash" }
func (p clef) USDPerMTokInput() float64 { return 0.09 }
func (p clef) Do(ctx context.Context, body []byte) (Result, error) {
	r, err := post(ctx, p.url, p.token, body)
	if err != nil || r.Status != http.StatusOK {
		return r, err
	}
	var w struct {
		Result  json.RawMessage `json:"result"`
		Success bool            `json:"success"`
		Errors  json.RawMessage `json:"errors"`
	}
	if e := json.Unmarshal(r.Raw, &w); e != nil {
		r.ParseError = e.Error()
	} else if !w.Success || len(w.Result) == 0 {
		r.ParseError = "workers ai success=false: " + string(w.Errors)
	} else {
		fill(&r, w.Result)
	}
	return r, nil
}

type openai struct{ c decisions.Client }

func (p openai) Name() string { return "decisions" }

// BodyModel keeps Jev's model name: the adapter replaces it on the wire.
func (p openai) BodyModel() string        { return "jev-1.13.0" }
func (p openai) USDPerMTokInput() float64 { return decisions.USDPerMTokInput }
func (p openai) Do(ctx context.Context, body []byte) (Result, error) {
	d, err := p.c.Do(ctx, body)
	r := Result{Status: d.Status, Raw: d.Raw, ParseError: d.ParseError, Seconds: d.Seconds, RetryAfter: d.RetryAfter, SentSHA256: sha(d.WireBody)}
	if err == nil && d.Normalized != nil {
		fill(&r, d.Normalized)
	}
	return r, err
}
