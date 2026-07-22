package content

import "testing"

func TestExtractTextStrategy(t *testing.T) {
	text, err := ExtractDocument("strategy.md", []byte("# Cilj\n\nPovećati broj kvalitetnih upita kroz provjerene stručne teme."))
	if err != nil {
		t.Fatalf("ExtractDocument() returned an error: %v", err)
	}
	if text == "" {
		t.Fatal("expected extracted strategy text")
	}
}

func TestExtractRejectsLegacyBinaryOfficeFiles(t *testing.T) {
	if _, err := ExtractDocument("strategy.doc", []byte("legacy")); err != ErrUnsupportedDocument {
		t.Fatalf("expected ErrUnsupportedDocument, got %v", err)
	}
}
