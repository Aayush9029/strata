package sourcecontext

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestBuildSkipsHiddenGeneratedDirectories(t *testing.T) {
	root := t.TempDir()
	writeSwift(t, filepath.Join(root, "Sources", "Screen.swift"), `struct Screen { var body: some View { Text("Scan") } }`)
	writeSwift(t, filepath.Join(root, ".derived", "Generated.swift"), `struct Generated { let value = "Scan" }`)

	index, err := Build([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	contexts := index.Find("Scan", "Scan", 10)
	if len(contexts) != 1 {
		t.Fatalf("contexts = %#v", contexts)
	}
	if filepath.Base(contexts[0].File) != "Screen.swift" {
		t.Fatalf("unexpected context file: %#v", contexts[0])
	}
}

func writeSwift(t *testing.T, path string, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestFindMatchesInterpolatedLiterals(t *testing.T) {
	root := t.TempDir()
	writeSwift(t, filepath.Join(root, "Sources", "Detail.swift"), `struct Detail: View {
    var body: some View {
        Text("\(formattedSize(bytes, (unit))) available")
        Button("Season \(season.number)") { }
        Text("Say \"hi\" to \(name)")
        Text("Plain label")
    }
}`)

	index, err := Build([]string{root})
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		key            string
		role           string
		interpolations []string
	}{
		{"%@ available", "text", []string{"formattedSize(bytes, (unit))"}},
		{"Season %lld", "button", []string{"season.number"}},
		{`Say "hi" to %@`, "text", []string{"name"}},
		{"Plain label", "text", nil},
	}
	for _, test := range tests {
		contexts := index.Find(test.key, test.key, 3)
		if len(contexts) != 1 {
			t.Fatalf("%q contexts = %#v", test.key, contexts)
		}
		context := contexts[0]
		if context.Role != test.role || !slices.Equal(context.Interpolations, test.interpolations) || context.Type != "Detail" {
			t.Fatalf("%q context = %#v", test.key, context)
		}
	}
}

func TestNormalizeFormat(t *testing.T) {
	if got, want := NormalizeFormat("%1$@ has 100%% of %lld"), "\x00 has 100% of \x00"; got != want {
		t.Fatalf("normalized = %q, want %q", got, want)
	}
	if got := parseSwiftLiteral(`"\(a) and \(b(c)) \u{1F3AC}"`); got.Normalized != "\x00 and \x00 🎬" || !slices.Equal(got.Interpolations, []string{"a", "b(c)"}) {
		t.Fatalf("literal = %#v", got)
	}
}

func TestRoleForUsesClosestCall(t *testing.T) {
	line := `        NavigationLink("Settings") { Text("x") }`
	if got := roleFor(line, strings.Index(line, `"Settings"`)); got != "navigation link" {
		t.Fatalf("role = %q", got)
	}
	if got := roleFor(line, strings.Index(line, `"x"`)); got != "text" {
		t.Fatalf("role = %q", got)
	}
}
