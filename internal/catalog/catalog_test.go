package catalog

import (
	"os"
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

func TestValidateTranslationPreservesPlaceholders(t *testing.T) {
	if err := ValidateTranslation("Buy %1$@ for %2$d days and %3$lld points", "Comprar %1$@ durante %2$d dias e %3$lld pontos"); err != nil {
		t.Fatal(err)
	}
	if err := ValidateTranslation("Buy %1$@ for %2$d days and %3$lld points", "Comprar %@ durante %d dias e pontos"); err == nil {
		t.Fatal("expected placeholder mismatch")
	}
	if err := ValidateTranslation("You have %#@count@", "Tienes count"); err == nil {
		t.Fatal("expected Xcode substitution placeholder mismatch")
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
	if len(items) != 1 {
		t.Fatalf("items = %#v", items)
	}
	if items[0].Source != "%lld scans" || len(items[0].VariantPath) == 0 {
		t.Fatalf("unexpected variation item: %#v", items[0])
	}
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
