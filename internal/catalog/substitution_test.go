package catalog

import (
	"slices"
	"testing"
)

func TestCheckSubstitutionRepetition(t *testing.T) {
	tests := []struct {
		name  string
		root  string
		forms map[string][]string
		valid bool
	}{
		{"ja repeated", "%1$@ · %#@titles@件の作品がライブラリにあります", map[string][]string{"titles": {"%arg件の作品がライブラリにあります"}}, false},
		{"ja clean", "%1$@ · %#@titles@", map[string][]string{"titles": {"%arg件の作品がライブラリにあります"}}, true},
		{"de repeated", "%1$@ · %#@titles@ Titel in deiner Mediathek", map[string][]string{"titles": {"%arg Titel in deiner Mediathek", "%arg Titel in deiner Mediathek"}}, false},
		{"de clean", "%1$@ · %#@titles@", map[string][]string{"titles": {"%arg Titel in deiner Mediathek"}}, true},
		{"ko repeated", "%#@movies@편의 영화 및 %#@shows@개의 TV 프로그램", map[string][]string{"movies": {"%arg편의 영화"}, "shows": {"%arg개의 TV 프로그램"}}, false},
		{"ko clean", "%#@movies@ 및 %#@shows@", map[string][]string{"movies": {"%arg편의 영화"}, "shows": {"%arg개의 TV 프로그램"}}, true},
		{"zh repeated", "%#@movies@ 部电影，%#@shows@ 部剧集", map[string][]string{"movies": {"%arg 部电影"}, "shows": {"%arg 部剧集"}}, false},
		{"zh clean", "%#@movies@，%#@shows@", map[string][]string{"movies": {"%arg 部电影"}, "shows": {"%arg 部剧集"}}, true},
		{"shared short word is fine", "Löschen: %#@count@", map[string][]string{"count": {"%arg Datei", "%arg Dateien"}}, true},
		{"en clean", "Delete %#@count@", map[string][]string{"count": {"%arg file", "%arg files"}}, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := CheckSubstitutionRepetition(test.root, test.forms)
			if test.valid && err != nil {
				t.Fatalf("expected valid, got %v", err)
			}
			if !test.valid && err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestSubstitutionEntryIsOneGroupWithStructure(t *testing.T) {
	cat, err := Load(writeCatalog(t, `{
  "sourceLanguage": "en",
  "strings": {
    "%1$@ · %#@titles@": {
      "localizations": {
        "en": {
          "stringUnit": { "state": "translated", "value": "%1$@ · %#@titles@" },
          "substitutions": {
            "titles": {
              "argNum": 2,
              "formatSpecifier": "lld",
              "variations": {
                "plural": {
                  "one": { "stringUnit": { "state": "translated", "value": "%arg title in your library" } },
                  "other": { "stringUnit": { "state": "translated", "value": "%arg titles in your library" } }
                }
              }
            }
          }
        },
        "de": {
          "stringUnit": { "state": "translated", "value": "%1$@ · %#@titles@" },
          "substitutions": {
            "titles": {
              "argNum": 2,
              "formatSpecifier": "lld",
              "variations": {
                "plural": {
                  "one": { "stringUnit": { "state": "translated", "value": "%arg Titel in deiner Mediathek" } }
                }
              }
            }
          }
        }
      }
    }
  },
  "version": "1.0"
}`))
	if err != nil {
		t.Fatal(err)
	}
	items := cat.MissingItems("de", false)
	if len(items) != 3 {
		t.Fatalf("expected root and both forms, got %#v", items)
	}
	structure := items[0].Structure
	if items[0].VariantPath != nil || structure == nil {
		t.Fatalf("root item = %#v", items[0])
	}
	want := []string{"%1$@ · 1 title in your library", "%1$@ · 3 titles in your library"}
	if !slices.Equal(structure.RenderedExamples, want) {
		t.Fatalf("examples = %#v", structure.RenderedExamples)
	}
	if structure.Substitutions["titles"]["other"] != "%arg titles in your library" {
		t.Fatalf("structure = %#v", structure)
	}
	if items[1].Structure != nil {
		t.Fatal("only the root item carries the structure")
	}
}
