package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRuneRejectsOtherModels(t *testing.T) {
	for model, wantErr := range map[string]bool{"accounts/fireworks/routers/rune": false, "other": true} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "" {
				t.Errorf("unexpected Authorization header")
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"model":"` + model + `","answers":{"q":{"type":"noul","noul":0.75}},"usage":{"input_tokens":12,"cached_input_tokens":2,"output_tokens":0}}`))
		}))
		p := runeRoute{url: s.URL, model: "accounts/fireworks/routers/rune"}
		r, err := p.Do(context.Background(), []byte(`{"model":"accounts/fireworks/routers/rune","state":"x","questions":{"q":{"type":"noul","instructions":"x?"}}}`))
		s.Close()
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.Contains(r.ParseError, "unexpected model"); got != wantErr {
			t.Fatalf("model %q: ParseError=%q", model, r.ParseError)
		}
		if !wantErr && (r.InputTokens != 12 || !r.UsageReported) {
			t.Fatalf("result=%+v", r)
		}
	}
	if p, err := New("rune"); err != nil || p.USDPerMTokInput() != 0.042 || p.BodyModel() != "accounts/fireworks/routers/rune" {
		t.Fatalf("New(rune)=%v %v", p, err)
	}
}
