package provider

import (
	"encoding/json"
	"testing"
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
