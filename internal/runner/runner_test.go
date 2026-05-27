package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Aayush9029/strata/internal/catalog"
	"github.com/Aayush9029/strata/internal/provider"
	"github.com/Aayush9029/strata/internal/ui"
)

func TestLocalizeWithMockProvider(t *testing.T) {
	path := writeCatalog(t, 120)
	result, err := Localize(context.Background(), Config{
		CatalogPaths: []string{path},
		Languages:    []string{"es"},
		Names:        map[string]string{},
		BatchSize:    25,
	}, provider.Mock{}, ui.New(&bytes.Buffer{}, &bytes.Buffer{}))
	if err != nil {
		t.Fatal(err)
	}
	if result.Translated != 120 || result.Batches != 5 {
		t.Fatalf("unexpected result: %#v", result)
	}
	cat, err := catalog.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := cat.Strings["Key 42"].Localizations["es"].Value(); got != "[es] Source 42 %@" {
		t.Fatalf("translation = %q", got)
	}
}

type panicTranslator struct{}

func (panicTranslator) Translate(context.Context, provider.Request) (provider.Response, error) {
	panic("dry-run should not call provider")
}

func TestDryRunDoesNotCallProvider(t *testing.T) {
	path := writeCatalog(t, 12)
	result, err := Localize(context.Background(), Config{
		CatalogPaths: []string{path},
		Languages:    []string{"es"},
		Names:        map[string]string{},
		BatchSize:    5,
		DryRun:       true,
	}, panicTranslator{}, ui.New(&bytes.Buffer{}, &bytes.Buffer{}))
	if err != nil {
		t.Fatal(err)
	}
	if result.Translated != 0 || result.Planned != 12 {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestLoadContextJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "strata.json")
	body := provider.ProjectConfig{
		AppName:        "Blume",
		AppContext:     "Blume is a food scanner.",
		ProtectedTerms: []string{"GroceryScan"},
		Glossary:       []string{"paywall = purchase screen"},
		StyleGuide:     []string{"short button labels"},
	}
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	context, err := loadProjectConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if context.AppContext != body.AppContext || len(context.AllProtectedTerms()) != 2 || context.Glossary[0] != body.Glossary[0] {
		t.Fatalf("context = %#v", context)
	}
}

func TestValidateProtectedTerms(t *testing.T) {
	if err := validateProtectedTerms("Blume Pro is active", "Blume Pro está activo", []string{"Blume"}); err != nil {
		t.Fatal(err)
	}
	if err := validateProtectedTerms("blume Pro is active", "Bloom Pro está activo", []string{"Blume"}); err == nil {
		t.Fatal("expected protected term validation error")
	}
}

func BenchmarkLocalizeMockProvider(b *testing.B) {
	for _, count := range []int{100, 1000, 5000} {
		b.Run("strings_"+itoa(count), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				path := writeCatalog(b, count)
				_, err := Localize(context.Background(), Config{
					CatalogPaths: []string{path},
					Languages:    []string{"ja"},
					Names:        map[string]string{},
					BatchSize:    40,
				}, provider.Mock{}, ui.New(&bytes.Buffer{}, &bytes.Buffer{}))
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func writeCatalog(tb testing.TB, count int) string {
	tb.Helper()
	dir := tb.TempDir()
	path := filepath.Join(dir, "Localizable.xcstrings")
	var buffer bytes.Buffer
	buffer.WriteString(`{"sourceLanguage":"en","strings":{`)
	for i := 0; i < count; i++ {
		if i > 0 {
			buffer.WriteByte(',')
		}
		buffer.WriteString(`"Key ` + itoa(i) + `":{"localizations":{"en":{"stringUnit":{"state":"translated","value":"Source ` + itoa(i) + ` %@"}},"es":{"stringUnit":{"state":"new","value":""}}}}`)
	}
	buffer.WriteString(`},"version":"1.0"}`)
	if err := os.WriteFile(path, buffer.Bytes(), 0644); err != nil {
		tb.Fatal(err)
	}
	return path
}
