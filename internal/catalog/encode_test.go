package catalog

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteRoundTripsXcodeFormat(t *testing.T) {
	original, err := os.ReadFile(filepath.Join("testdata", "Format.xcstrings"))
	if err != nil {
		t.Fatal(err)
	}
	path := writeCatalog(t, string(original))
	cat, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := cat.Write(); err != nil {
		t.Fatal(err)
	}
	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(written, original) {
		t.Fatalf("round trip changed the file:\n%s", written)
	}
}

func TestWriteDoesNotEscapeHTML(t *testing.T) {
	path := writeCatalog(t, `{"sourceLanguage":"en","strings":{"A & B":{"localizations":{"en":{"stringUnit":{"state":"translated","value":"<A & B>"}}}}},"version":"1.0"}`)
	cat, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	cat.ApplyTranslation("de", "A & B", nil, "<A & B>")
	if err := cat.Write(); err != nil {
		t.Fatal(err)
	}
	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(written)
	if strings.Contains(text, `\u0026`) || strings.Contains(text, `\u003c`) || !strings.Contains(text, `"value" : "<A & B>"`) {
		t.Fatalf("unexpected output:\n%s", text)
	}
	if !strings.HasSuffix(text, "}\n") {
		t.Fatal("missing trailing newline")
	}
}
