package provider

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestParseCost(t *testing.T) {
	if got := parseCost(json.RawMessage(`0.00001452`)); got != 0.00001452 {
		t.Fatalf("number cost = %v", got)
	}
	if got := parseCost(json.RawMessage(`"0.25"`)); got != 0.25 {
		t.Fatalf("string cost = %v", got)
	}
	if got := parseCost(json.RawMessage(`null`)); got != 0 {
		t.Fatalf("null cost = %v", got)
	}
}

func TestProjectConfigAllProtectedTerms(t *testing.T) {
	config := ProjectConfig{
		AppName:        "Blume",
		ProtectedTerms: []string{"Blume", "GroceryScan", " "},
	}
	got := config.AllProtectedTerms()
	if len(got) != 2 || got[0] != "Blume" || got[1] != "GroceryScan" {
		t.Fatalf("terms = %#v", got)
	}
}

func TestPromptRules(t *testing.T) {
	rules := strings.Join(promptRules(ProjectConfig{AppName: "Marquee"}), "\n")
	for _, want := range []string{
		"${applicationName}",
		"%1$@ and %2$@ (keep the type, e.g. %2$lld)",
		"CLDR plural category",
		"previous_error",
		"Einstellungen",
		"trailing punctuation",
		"leading and trailing whitespace",
		"typographic quotation marks",
		"Do not translate this term: Marquee.",
	} {
		if !strings.Contains(rules, want) {
			t.Errorf("rules do not mention %q", want)
		}
	}
}

func TestOpenRouterResponseErrors(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		body      string
		retryable bool
		response  bool
		contains  string
	}{
		{"no choices", 200, `{"choices":[],"error":{"message":"upstream overloaded"}}`, true, true, "upstream overloaded"},
		{"truncated JSON", 200, `{"choices":[{"finish_reason":"length","message":{"content":"{\"0\": \"Hal"}}]}`, true, true, `finish_reason "length"`},
		{"bad key", 401, `{"error":"invalid key"}`, false, false, "invalid key"},
		{"rate limit", 429, `{"error":"slow down"}`, true, false, "slow down"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()
			client := NewOpenRouter("key", "model", 5*time.Second)
			client.Endpoint = server.URL
			_, err := client.Translate(context.Background(), Request{LanguageName: "German"})
			if err == nil {
				t.Fatal("expected an error")
			}
			var responseErr *ResponseError
			if errors.As(err, &responseErr) != test.response || Retryable(err) != test.retryable || !strings.Contains(err.Error(), test.contains) {
				t.Fatalf("err = %v (response %v, retryable %v)", err, errors.As(err, &responseErr), Retryable(err))
			}
		})
	}
}
