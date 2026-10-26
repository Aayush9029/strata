package catalog

import (
	"slices"
	"testing"
)

func TestPlaceholdersIncludesAppShortcutsTokensAndSkipsPercentLiteral(t *testing.T) {
	got := Placeholders("Open ${applicationName} at 100%% with %lld items and %#@count@ %arg")
	want := []string{"${applicationName}", "%lld", "%#@count@", "%arg"}
	if !slices.Equal(got, want) {
		t.Fatalf("placeholders = %#v, want %#v", got, want)
	}
}

func TestValidateTranslation(t *testing.T) {
	tests := []struct {
		name        string
		source      string
		translation string
		valid       bool
	}{
		{"same positional", "Buy %1$@ for %2$d days and %3$lld points", "Comprar %1$@ durante %2$d dias e %3$lld pontos", true},
		{"missing arguments", "Buy %1$@ for %2$d days and %3$lld points", "Comprar %@ durante %d dias e pontos", false},
		{"missing substitution", "You have %#@count@", "Tienes count", false},
		{"positional reorder", "%@ shared %@", "%2$@ が %1$@ を共有しました", true},
		{"positional reorder keeps type", "%@ watched %lld episodes", "%2$lld Folgen hat %1$@ gesehen", true},
		{"non-positional reorder changes types", "%@ watched %lld episodes", "%lld Folgen hat %@ gesehen", false},
		{"mixed reorder", "%@ shared %@", "%2$@ が %@ を共有しました", false},
		{"non-positional same order", "%1$@ and %2$lld", "%@ und %lld", true},
		{"wrong length", "Season %lld", "Staffel %d", false},
		{"percent literal ignored", "100%% done in %@", "%@ で 100%% 完了", true},
		{"escape ignored", `Line\nbreak %@`, "Zeile %@", true},
		{"app shortcuts token kept", "Play in ${applicationName}", "${applicationName} で再生", true},
		{"app shortcuts token dropped", "Play in ${applicationName}", "Reproducir", false},
		{"app shortcuts token renamed", "Play in ${applicationName}", "Reproducir en ${appName}", false},
		{"repeated positional", "%1$@ and %1$@", "%1$@ y %1$@", true},
		{"%i equals %d", "%i items", "%d Elemente", true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateTranslation(test.source, test.translation)
			if test.valid && err != nil {
				t.Fatalf("expected valid, got %v", err)
			}
			if !test.valid && err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}
