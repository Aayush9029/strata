package catalog

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

const pluralCatalog = `{
  "sourceLanguage": "en",
  "strings": {
    "%lld episodes": {
      "localizations": {
        "en": {
          "variations": {
            "plural": {
              "one": { "stringUnit": { "state": "translated", "value": "%lld episode" } },
              "other": { "stringUnit": { "state": "translated", "value": "%lld episodes" } }
            }
          }
        }
      }
    },
    "Delete %#@count@": {
      "localizations": {
        "en": {
          "stringUnit": { "state": "translated", "value": "Delete %#@count@" },
          "substitutions": {
            "count": {
              "argNum": 1,
              "formatSpecifier": "lld",
              "variations": {
                "plural": {
                  "one": { "stringUnit": { "state": "translated", "value": "%arg file" } },
                  "other": { "stringUnit": { "state": "translated", "value": "%arg files" } },
                  "zero": { "stringUnit": { "state": "translated", "value": "no files" } }
                }
              }
            }
          }
        }
      }
    },
    "Tap to continue": {
      "localizations": {
        "en": {
          "variations": {
            "device": {
              "iphone": { "stringUnit": { "state": "translated", "value": "Tap to continue" } },
              "mac": { "stringUnit": { "state": "translated", "value": "Click to continue" } }
            }
          }
        }
      }
    },
    "Marquee": {
      "shouldTranslate": false
    }
  },
  "version": "1.0"
}`

func variantPaths(items []Item, key string) []string {
	var paths []string
	for _, item := range items {
		if item.Key == key {
			paths = append(paths, strings.Join(item.VariantPath, "."))
		}
	}
	return paths
}

func TestPluralCategoriesUseTargetLanguage(t *testing.T) {
	cat, err := Load(writeCatalog(t, pluralCatalog))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		language string
		want     []string
	}{
		{"ja", []string{"variations.plural.other.stringUnit"}},
		{"zh-Hans", []string{"variations.plural.other.stringUnit"}},
		{"de", []string{"variations.plural.one.stringUnit", "variations.plural.other.stringUnit"}},
		{"fr", []string{"variations.plural.one.stringUnit", "variations.plural.many.stringUnit", "variations.plural.other.stringUnit"}},
		{"pt-BR", []string{"variations.plural.one.stringUnit", "variations.plural.many.stringUnit", "variations.plural.other.stringUnit"}},
		{"ru", []string{"variations.plural.one.stringUnit", "variations.plural.few.stringUnit", "variations.plural.many.stringUnit", "variations.plural.other.stringUnit"}},
		{"xx", []string{"variations.plural.one.stringUnit", "variations.plural.other.stringUnit"}},
	}
	for _, test := range tests {
		got := variantPaths(cat.MissingItems(test.language, false), "%lld episodes")
		if !slices.Equal(got, test.want) {
			t.Errorf("%s paths = %v, want %v", test.language, got, test.want)
		}
	}
}

func TestPluralMissingCategoryUsesOtherSource(t *testing.T) {
	cat, err := Load(writeCatalog(t, pluralCatalog))
	if err != nil {
		t.Fatal(err)
	}
	sources := map[string]string{}
	for _, item := range cat.MissingItems("ar", false) {
		sources[strings.Join(item.VariantPath, ".")] = item.Source
	}
	checks := map[string]string{
		"variations.plural.zero.stringUnit":                     "%lld episodes",
		"variations.plural.two.stringUnit":                      "%lld episodes",
		"variations.plural.one.stringUnit":                      "%lld episode",
		"substitutions.count.variations.plural.zero.stringUnit": "no files",
		"substitutions.count.variations.plural.few.stringUnit":  "%arg files",
	}
	for path, want := range checks {
		if got := sources[path]; got != want {
			t.Errorf("%s source = %q, want %q", path, got, want)
		}
	}
}

func TestSubstitutionAndDeviceLeaves(t *testing.T) {
	cat, err := Load(writeCatalog(t, pluralCatalog))
	if err != nil {
		t.Fatal(err)
	}
	items := cat.MissingItems("ja", false)
	if got, want := variantPaths(items, "Delete %#@count@"), []string{"", "substitutions.count.variations.plural.other.stringUnit"}; !slices.Equal(got, want) {
		t.Fatalf("substitution paths = %v, want %v", got, want)
	}
	if got, want := variantPaths(items, "Tap to continue"), []string{"variations.device.iphone.stringUnit", "variations.device.mac.stringUnit"}; !slices.Equal(got, want) {
		t.Fatalf("device paths = %v, want %v", got, want)
	}

	for _, item := range items {
		cat.ApplyTranslation("ja", item.Key, item.VariantPath, "[ja] "+item.Source)
	}
	if remaining := cat.MissingItems("ja", false); len(remaining) != 0 {
		t.Fatalf("remaining = %#v", remaining)
	}
	if err := cat.Write(); err != nil {
		t.Fatal(err)
	}
	reloaded, err := Load(cat.Path)
	if err != nil {
		t.Fatal(err)
	}
	ja := reloaded.Strings["Delete %#@count@"].Localizations["ja"]
	if ja.Value() != "[ja] Delete %#@count@" {
		t.Fatalf("stringUnit = %q", ja.Value())
	}
	count := ja.raw["substitutions"].(map[string]any)["count"].(map[string]any)
	if count["argNum"] != json.Number("1") || count["formatSpecifier"] != "lld" {
		t.Fatalf("substitution fields were not copied: %#v", count)
	}
	plural := count["variations"].(map[string]any)["plural"].(map[string]any)
	if len(plural) != 1 || plural["other"] == nil {
		t.Fatalf("ja plural categories = %v", sortedKeys(plural))
	}
	if coverage := reloaded.Coverage("ja"); coverage.Missing != 0 || coverage.Total != 5 {
		t.Fatalf("coverage = %#v", coverage)
	}
}

func TestShouldTranslateFalseIsSkipped(t *testing.T) {
	cat, err := Load(writeCatalog(t, pluralCatalog))
	if err != nil {
		t.Fatal(err)
	}
	if paths := variantPaths(cat.MissingItems("de", false), "Marquee"); len(paths) != 0 {
		t.Fatalf("Marquee should be skipped, got %v", paths)
	}
	if coverage := cat.Coverage("de"); coverage.Total != 7 {
		t.Fatalf("coverage total = %d, want 7", coverage.Total)
	}
}
