package sourcecontext

import (
	"os"
	"path/filepath"
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
