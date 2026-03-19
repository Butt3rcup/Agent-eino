package parser

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ledongthuc/pdf"
)

func (p *Parser) ParseMarkdown(content []byte) (string, error) {
	return string(content), nil
}

func (p *Parser) ParsePDF(filePath string) (string, error) {
	f, r, err := pdf.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to open PDF: %w", err)
	}
	defer f.Close()

	var buf bytes.Buffer
	totalPage := r.NumPage()

	for pageIndex := 1; pageIndex <= totalPage; pageIndex++ {
		p := r.Page(pageIndex)
		if p.V.IsNull() {
			continue
		}

		text, err := p.GetPlainText(nil)
		if err != nil {
			continue
		}

		buf.WriteString(text)
		buf.WriteString("\n")
	}

	return buf.String(), nil
}

func (p *Parser) ParseFile(filePath string) (string, error) {
	ext := strings.ToLower(filepath.Ext(filePath))
	if ext == "" {
		return "", fmt.Errorf("unsupported file type: %s", filePath)
	}

	switch ext {
	case ".md", ".markdown":
		content, err := os.ReadFile(filePath)
		if err != nil {
			return "", fmt.Errorf("failed to read markdown: %w", err)
		}
		return p.ParseMarkdown(content)
	case ".pdf":
		return p.ParsePDF(filePath)
	default:
		return "", fmt.Errorf("unsupported file type: %s", ext)
	}
}

func (p *Parser) ParseReader(r io.Reader, fileType string) (string, error) {
	content, err := io.ReadAll(r)
	if err != nil {
		return "", fmt.Errorf("failed to read content: %w", err)
	}

	switch strings.ToLower(fileType) {
	case ".md", ".markdown", "markdown":
		return p.ParseMarkdown(content)
	default:
		return "", fmt.Errorf("unsupported file type for reader: %s", fileType)
	}
}
