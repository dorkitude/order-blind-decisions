package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSubQDirectRoute(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization=%q", got)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode: %v", err)
		}
		if body["model"] != subqModel {
			t.Errorf("model=%v", body["model"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"subq-snap-preview","answers":{"q":{"type":"noul","noul":0.75}},"usage":{"input_tokens":12,"cached_input_tokens":2,"output_tokens":0}}`))
	}))
	defer s.Close()
	t.Setenv("SUBQ_URL", s.URL)
	t.Setenv("SUBQ_API_KEY", "test-key")
	p, err := New("subq")
	if err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"model":"subq-snap-preview","state":"x","questions":{"q":{"type":"noul","instructions":"x?"}}}`)
	r, err := p.Do(context.Background(), body)
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != http.StatusOK || r.ParseError != "" || r.ResponseModel != subqModel || r.InputTokens != 12 || !r.UsageReported {
		t.Fatalf("result=%+v", r)
	}
}

func TestSubQConfigGuards(t *testing.T) {
	t.Setenv("SUBQ_URL", subqDirectURL)
	t.Setenv("SUBQ_API_KEY", "")
	if _, err := New("subq"); err == nil {
		t.Fatal("direct route accepted without key")
	}
	t.Setenv("SUBQ_API_KEY", "test")
	t.Setenv("SUBQ_USD_PER_MTOK_INPUT", "not-a-price")
	if _, err := New("subq"); err == nil {
		t.Fatal("invalid price accepted")
	}
}
