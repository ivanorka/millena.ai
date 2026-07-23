package content

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	pdf "github.com/ledongthuc/pdf"
)

const maxStrategyTextRunes = 120000

var ErrUnsupportedDocument = errors.New("unsupported strategy document")
var ErrNoDocumentText = errors.New("strategy document contains no extractable text")

func ExtractDocument(filename string, data []byte) (string, error) {
	extension := strings.ToLower(filepath.Ext(filename))
	var text string
	var err error
	switch extension {
	case ".txt", ".md", ".csv", ".json", ".html", ".htm":
		text = string(data)
	case ".pdf":
		text, err = extractPDF(data)
	case ".docx":
		text, err = extractOfficeXML(data, "word/")
	case ".pptx":
		text, err = extractOfficeXML(data, "ppt/slides/")
	default:
		return "", ErrUnsupportedDocument
	}
	if err != nil {
		return "", err
	}
	text = normalizeExtractedText(text)
	if utf8.RuneCountInString(text) < 20 {
		return "", ErrNoDocumentText
	}
	if utf8.RuneCountInString(text) > maxStrategyTextRunes {
		text = string([]rune(text)[:maxStrategyTextRunes])
	}
	return text, nil
}

func extractPDF(data []byte) (string, error) {
	reader, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("open PDF: %w", err)
	}
	fonts := make(map[string]*pdf.Font)
	pages := make([]string, 0, reader.NumPage())
	for pageNumber := 1; pageNumber <= reader.NumPage(); pageNumber++ {
		page := reader.Page(pageNumber)
		for _, name := range page.Fonts() {
			if _, exists := fonts[name]; !exists {
				font := page.Font(name)
				fonts[name] = &font
			}
		}
		text, err := page.GetPlainText(fonts)
		if err != nil {
			return "", fmt.Errorf("extract PDF page %d: %w", pageNumber, err)
		}
		pages = append(pages, text)
	}
	// The separator stays in the stored plain text so a team can see where a
	// PDF page ended while reviewing or editing the extracted strategy.
	return strings.Join(pages, "\n\n------------------------------\n\n"), nil
}

func extractOfficeXML(data []byte, prefix string) (string, error) {
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("open Office document: %w", err)
	}
	files := make([]*zip.File, 0)
	for _, file := range archive.File {
		name := strings.ToLower(file.Name)
		if strings.HasPrefix(name, prefix) && strings.HasSuffix(name, ".xml") {
			if prefix == "word/" && name != "word/document.xml" && !strings.HasPrefix(name, "word/header") && !strings.HasPrefix(name, "word/footer") {
				continue
			}
			files = append(files, file)
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	var result strings.Builder
	for _, file := range files {
		opened, err := file.Open()
		if err != nil {
			return "", err
		}
		decoder := xml.NewDecoder(io.LimitReader(opened, 4<<20))
		for {
			token, err := decoder.Token()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				_ = opened.Close()
				return "", fmt.Errorf("parse Office document: %w", err)
			}
			if characters, ok := token.(xml.CharData); ok {
				value := strings.TrimSpace(string(characters))
				if value != "" {
					result.WriteString(value)
					result.WriteByte(' ')
				}
			}
		}
		_ = opened.Close()
		result.WriteString("\n\n")
	}
	return result.String(), nil
}

func normalizeExtractedText(value string) string {
	value = strings.Map(func(r rune) rune {
		switch {
		case r == '\x00':
			return -1
		case r == '\r':
			return '\n'
		case unicode.IsControl(r) && r != '\n' && r != '\t':
			return ' '
		default:
			return r
		}
	}, value)
	lines := strings.Split(value, "\n")
	normalized := make([]string, 0, len(lines))
	lastBlank := true
	for _, line := range lines {
		line = strings.TrimSpace(strings.Join(strings.Fields(line), " "))
		if line == "" {
			if !lastBlank {
				normalized = append(normalized, "")
			}
			lastBlank = true
			continue
		}
		normalized = append(normalized, line)
		lastBlank = false
	}
	return strings.TrimSpace(strings.Join(normalized, "\n"))
}
