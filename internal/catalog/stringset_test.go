package catalog

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func loadAppShortcuts(t *testing.T) (*Catalog, []byte) {
	t.Helper()
	original, err := os.ReadFile(filepath.Join("testdata", "AppShortcuts.xcstrings"))
	if err != nil {
		t.Fatal(err)
	}
	cat, err := Load(writeCatalog(t, string(original)))
	if err != nil {
		t.Fatal(err)
	}
	return cat, original
}

func TestStringSetRoundTrip(t *testing.T) {
	cat, original := loadAppShortcuts(t)
	if err := cat.Write(); err != nil {
		t.Fatal(err)
	}
	written, err := os.ReadFile(cat.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(written, original) {
		t.Fatalf("round trip changed the file:\n%s", written)
	}
}

func TestStringSetItems(t *testing.T) {
	cat, _ := loadAppShortcuts(t)
	if items := cat.MissingItems("de", false); len(items) != 2 {
		t.Fatalf("de items = %#v", items)
	}
	items := cat.MissingItems("ja", false)
	got := map[string]string{}
	for _, item := range items {
		got[item.Key+" | "+strings.Join(item.VariantPath, ".")] = item.Source
		if strings.Contains(item.Source, "${applicationName}") && !slices.Contains(item.Placeholders, "${applicationName}") {
			t.Fatalf("placeholders = %v", item.Placeholders)
		}
	}
	want := map[string]string{
		"Continue watching in ${applicationName} | stringSet.values.0": "Continue watching in ${applicationName}",
		"Continue watching in ${applicationName} | stringSet.values.1": "Pick up where I left off in ${applicationName}",
		"Open ${applicationName} | stringSet.values.0":                 "Open ${applicationName}",
		"Continue Watching | ": "Continue Watching",
	}
	if len(got) != len(want) {
		t.Fatalf("items = %v", got)
	}
	for key, source := range want {
		if got[key] != source {
			t.Fatalf("%s source = %q, want %q (all: %v)", key, got[key], source, got)
		}
	}
}

func TestStringSetApplyKeepsOrderAndCount(t *testing.T) {
	cat, _ := loadAppShortcuts(t)
	key := "Continue watching in ${applicationName}"
	cat.ApplyTranslation("ja", key, []string{"stringSet", "values", "1"}, "${applicationName}で続きから再生")
	set := cat.Strings[key].Localizations["ja"].raw["stringSet"].(map[string]any)
	if set["state"] != "new" || len(set["values"].([]any)) != 2 {
		t.Fatalf("partial set = %#v", set)
	}
	if coverage := cat.Coverage("ja"); coverage.Translated != 1 {
		t.Fatalf("coverage = %#v", coverage)
	}
	if items := cat.MissingItems("ja", false); len(items) != 3 || items[1].VariantPath[2] != "0" {
		t.Fatalf("items = %#v", items)
	}

	cat.ApplyTranslation("ja", key, []string{"stringSet", "values", "0"}, "${applicationName}で視聴を続ける")
	cat.ApplyTranslation("ja", "Open ${applicationName}", []string{"stringSet", "values", "0"}, "${applicationName}を開く")
	if err := cat.Write(); err != nil {
		t.Fatal(err)
	}
	reloaded, err := Load(cat.Path)
	if err != nil {
		t.Fatal(err)
	}
	set = reloaded.Strings[key].Localizations["ja"].raw["stringSet"].(map[string]any)
	values := set["values"].([]any)
	if set["state"] != "translated" || len(values) != 2 || values[0] != "${applicationName}で視聴を続ける" || values[1] != "${applicationName}で続きから再生" {
		t.Fatalf("set = %#v", set)
	}
	open := reloaded.Strings["Open ${applicationName}"].Localizations["ja"].raw
	if _, ok := open["stringUnit"]; ok {
		t.Fatalf("expected a stringSet, got %#v", open)
	}
	if values, _ := stringSetValues(open); len(values) != 1 || values[0] != "${applicationName}を開く" {
		t.Fatalf("open = %#v", open)
	}
}

func TestStringSetValidationKeepsApplicationName(t *testing.T) {
	if err := ValidateTranslation("Pick up where I left off in ${applicationName}", "Dort weitermachen, wo ich aufgehört habe"); err == nil {
		t.Fatal("expected an error when ${applicationName} is dropped")
	}
}
