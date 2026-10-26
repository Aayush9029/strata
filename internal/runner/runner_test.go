package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

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
	tests := []struct {
		name        string
		source      string
		translation string
		terms       []string
		valid       bool
	}{
		{"kept", "Blume Pro is active", "Blume Pro está activo", []string{"Blume"}, true},
		{"changed", "Blume Pro is active", "Bloom Pro está activo", []string{"Blume"}, false},
		{"lowercase source is not the term", "blume Pro is active", "Bloom Pro está activo", []string{"Blume"}, true},
		{"substring in source is ignored", "This is complex", "Esto es complejo", []string{"Plex"}, true},
		{"substring in source is ignored for Box", "Show Checkbox", "Kontrollkästchen anzeigen", []string{"Box"}, true},
		{"substring in translation does not count", "Open Plex", "Abrir Complexo", []string{"Plex"}, false},
		{"case changed", "Open Plex", "Abrir PLEX", []string{"Plex"}, false},
		{"phrase kept", "Connect Google Drive", "Google Drive verbinden", []string{"Google Drive"}, true},
		{"phrase split", "Connect Google Drive", "Google-Laufwerk verbinden", []string{"Google Drive"}, false},
		{"punctuation boundary", "Watch on Apple TV.", "Ver en «Apple TV».", []string{"Apple TV"}, true},
		{"cjk neighbor", "Open Plex", "Plexを開く", []string{"Plex"}, true},
		{"hangul neighbor", "Open Plex", "Plex에서 열기", []string{"Plex"}, true},
		{"second occurrence matches", "Plex", "Plexus Plex", []string{"Plex"}, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateProtectedTerms(test.source, test.translation, test.terms)
			if test.valid && err != nil {
				t.Fatalf("expected valid, got %v", err)
			}
			if !test.valid && err == nil {
				t.Fatal("expected an error")
			}
		})
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

func TestLocalizeConcurrentBatches(t *testing.T) {
	path := writeCatalog(t, 200)
	result, err := Localize(context.Background(), Config{
		CatalogPaths: []string{path},
		Languages:    []string{"es"},
		Names:        map[string]string{},
		BatchSize:    10,
		Concurrency:  6,
	}, provider.Mock{}, ui.New(&bytes.Buffer{}, &bytes.Buffer{}))
	if err != nil {
		t.Fatal(err)
	}
	if result.Translated != 200 || result.Batches != 20 {
		t.Fatalf("unexpected result: %#v", result)
	}
	cat, err := catalog.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if coverage := cat.Coverage("es"); coverage.Missing != 0 {
		t.Fatalf("coverage = %#v", coverage)
	}
}

// flakyTranslator drops the placeholder for "Key 1" until the request reports
// the previous error, and always drops it for "Key 2".
type flakyTranslator struct {
	mu       sync.Mutex
	requests int
}

func (f *flakyTranslator) Translate(_ context.Context, request provider.Request) (provider.Response, error) {
	f.mu.Lock()
	f.requests++
	f.mu.Unlock()
	result := map[string]string{}
	for _, item := range request.Items {
		text := "[" + request.LanguageCode + "] " + item.Source
		if (item.Key == "Key 1" && item.PreviousError == "") || item.Key == "Key 2" {
			text = "[" + request.LanguageCode + "] no placeholder"
		}
		result[item.ID] = text
	}
	return provider.Response{Translations: result}, nil
}

func TestLocalizeRetriesAndSkipsInvalidItems(t *testing.T) {
	path := writeCatalog(t, 5)
	translator := &flakyTranslator{}
	result, err := Localize(context.Background(), Config{
		CatalogPaths: []string{path},
		Languages:    []string{"es"},
		Names:        map[string]string{},
		BatchSize:    10,
		Concurrency:  2,
	}, translator, ui.New(&bytes.Buffer{}, &bytes.Buffer{}))
	if err != nil {
		t.Fatal(err)
	}
	if result.Translated != 4 || len(result.Skipped) != 1 || result.Skipped[0].Key != "Key 2" {
		t.Fatalf("unexpected result: %#v", result)
	}
	if !strings.Contains(result.Skipped[0].Reason, "placeholder mismatch") {
		t.Fatalf("reason = %q", result.Skipped[0].Reason)
	}
	if translator.requests != 3 {
		t.Fatalf("requests = %d, want 3", translator.requests)
	}
	cat, err := catalog.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := cat.Strings["Key 1"].Localizations["es"].Value(); got != "[es] Source 1 %@" {
		t.Fatalf("Key 1 = %q", got)
	}
	if got := cat.Strings["Key 2"].Localizations["es"].Value(); got != "" {
		t.Fatalf("Key 2 = %q", got)
	}
}

// failingTranslator succeeds for the first batch and fails every later request.
type failingTranslator struct {
	mu    sync.Mutex
	calls int
}

func (f *failingTranslator) Translate(ctx context.Context, request provider.Request) (provider.Response, error) {
	f.mu.Lock()
	f.calls++
	first := f.calls == 1
	f.mu.Unlock()
	if !first {
		return provider.Response{}, errors.New("transport failed")
	}
	return provider.Mock{}.Translate(ctx, request)
}

func TestLocalizeProviderFailureKeepsCompletedBatches(t *testing.T) {
	path := writeCatalog(t, 20)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	translator := &failingTranslator{}
	done := make(chan struct{})
	var result Result
	var err error
	go func() {
		result, err = Localize(ctx, Config{
			CatalogPaths: []string{path},
			Languages:    []string{"es"},
			Names:        map[string]string{},
			BatchSize:    10,
			Concurrency:  1,
		}, translator, ui.New(&bytes.Buffer{}, &bytes.Buffer{}))
		close(done)
	}()
	// The retry backoff waits seconds; cancel to keep the test fast.
	time.Sleep(200 * time.Millisecond)
	cancel()
	<-done
	if err == nil {
		t.Fatal("expected an error")
	}
	if result.Translated != 10 {
		t.Fatalf("translated = %d, want 10", result.Translated)
	}
	cat, loadErr := catalog.Load(path)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if coverage := cat.Coverage("es"); coverage.Translated != 10 {
		t.Fatalf("coverage = %#v", coverage)
	}
}

func TestParseArgsConcurrency(t *testing.T) {
	config, err := parseArgs([]string{"--concurrency", "8"})
	if err != nil {
		t.Fatal(err)
	}
	if config.Concurrency != 8 {
		t.Fatalf("concurrency = %d", config.Concurrency)
	}
	config, err = parseArgs(nil)
	if err != nil || config.Concurrency != 4 {
		t.Fatalf("default concurrency = %d, err = %v", config.Concurrency, err)
	}
	if _, err := parseArgs([]string{"--concurrency", "0"}); err == nil {
		t.Fatal("expected an error for zero concurrency")
	}
}

// shortcutTranslator drops ${applicationName} from the second phrase.
type shortcutTranslator struct{}

func (shortcutTranslator) Translate(_ context.Context, request provider.Request) (provider.Response, error) {
	result := map[string]string{}
	for _, item := range request.Items {
		text := "[" + request.LanguageCode + "] " + item.Source
		if strings.HasSuffix(strings.Join(item.VariantPath, "."), "values.1") {
			text = "[" + request.LanguageCode + "] no app name"
		}
		result[item.ID] = text
	}
	return provider.Response{Translations: result}, nil
}

func TestLocalizeStringSetSkipsPhraseWithoutApplicationName(t *testing.T) {
	path := filepath.Join(t.TempDir(), "AppShortcuts.xcstrings")
	body := `{"sourceLanguage":"en","strings":{"Play in ${applicationName}":{"localizations":{"en":{"stringSet":{"state":"translated","values":["Play in ${applicationName}","Start ${applicationName}"]}}}}},"version":"1.0"}`
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	result, err := Localize(context.Background(), Config{
		CatalogPaths: []string{path},
		Languages:    []string{"es"},
		Names:        map[string]string{},
		BatchSize:    10,
	}, shortcutTranslator{}, ui.New(&bytes.Buffer{}, &bytes.Buffer{}))
	if err != nil {
		t.Fatal(err)
	}
	if result.Translated != 1 || len(result.Skipped) != 1 || result.Skipped[0].Variant != "stringSet.values.1" {
		t.Fatalf("unexpected result: %#v", result)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"[es] Play in ${applicationName}"`) || !strings.Contains(string(data), `"state" : "new"`) {
		t.Fatalf("catalog:\n%s", data)
	}
}
