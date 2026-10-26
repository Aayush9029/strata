package catalog

import (
	"os"
	"slices"
	"path/filepath"
	"testing"
)

func TestMissingItemsSkipsExistingUnlessForced(t *testing.T) {
	path := writeCatalog(t, `{
  "sourceLanguage": "en",
  "strings": {
    "Hello %@": {
      "localizations": {
        "en": { "stringUnit": { "state": "translated", "value": "Hello %@" } },
        "es": { "stringUnit": { "state": "translated", "value": "Hola %@" } }
      }
    },
    "Scan": {
      "localizations": {
        "en": { "stringUnit": { "state": "translated", "value": "Scan" } },
        "es": { "stringUnit": { "state": "new", "value": "" } }
      }
    }
  },
  "version": "1.0"
}`)
	cat, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	items := cat.MissingItems("es", false)
	if len(items) != 1 || items[0].Key != "Scan" {
		t.Fatalf("expected only Scan, got %#v", items)
	}

	items = cat.MissingItems("es", true)
	if len(items) != 2 {
		t.Fatalf("expected all items when forced, got %#v", items)
	}
}

func TestApplyAndWritePreservesRootFields(t *testing.T) {
	path := writeCatalog(t, `{
  "sourceLanguage": "en",
  "strings": {
    "Scan": {
      "comment": "button",
      "localizations": {
        "en": { "stringUnit": { "state": "translated", "value": "Scan" } }
      }
    }
  },
  "version": "1.0"
}`)
	cat, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	cat.ApplyTranslation("ja", "Scan", nil, "スキャン")
	if err := cat.Write(); err != nil {
		t.Fatal(err)
	}
	reloaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded.Strings["Scan"].Localizations["ja"].Value(); got != "スキャン" {
		t.Fatalf("translation = %q", got)
	}
	if reloaded.originalObject["version"] != "1.0" {
		t.Fatal("version was not preserved")
	}
}

func TestVariationEntriesUseSourceLeaves(t *testing.T) {
	path := writeCatalog(t, `{
  "sourceLanguage": "en",
  "strings": {
    "%lld scans": {
      "localizations": {
        "en": {
          "variations": {
            "plural": {
              "one": { "stringUnit": { "state": "translated", "value": "%lld scan" } },
              "other": { "stringUnit": { "state": "translated", "value": "%lld scans" } }
            }
          }
        },
        "es": {
          "variations": {
            "plural": {
              "one": { "stringUnit": { "state": "translated", "value": "%lld escaneo" } },
              "other": { "stringUnit": { "state": "new", "value": "" } }
            }
          }
        }
      }
    }
  },
  "version": "1.0"
}`)
	cat, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	items := cat.MissingItems("es", false)
	if len(items) != 2 {
		t.Fatalf("items = %#v", items)
	}
	if items[1].Source != "%lld scans" || !slices.Equal(items[1].VariantPath, []string{"variations", "plural", "other", "stringUnit"}) {
		t.Fatalf("unexpected variation item: %#v", items[1])
	}
	items = items[1:]
	cat.ApplyTranslation("es", items[0].Key, items[0].VariantPath, "%lld escaneos")
	if err := cat.Write(); err != nil {
		t.Fatal(err)
	}
	reloaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	leaf, ok := findStringUnitLeaf(reloaded.Strings["%lld scans"].Localizations["es"], items[0].VariantPath)
	if !ok || leaf.Value != "%lld escaneos" {
		t.Fatalf("leaf = %#v ok=%v", leaf, ok)
	}
}

func writeCatalog(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "Localizable.xcstrings")
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}
