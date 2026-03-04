package parser

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/ledongthuc/pdf"
)

type Parser struct {
}

func NewParser() *Parser {
	return &Parser{}
}

func (p *Parser) Close() {
	// No cleanup needed without jieba
}

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

func (p *Parser) Tokenize(text string) []string {
	// Simple tokenization without jieba - split on whitespace and punctuation
	var tokens []string
	var currentToken strings.Builder

	for _, r := range text {
		if unicode.IsSpace(r) || unicode.IsPunct(r) {
			if currentToken.Len() > 0 {
				tokens = append(tokens, currentToken.String())
				currentToken.Reset()
			}
		} else {
			currentToken.WriteRune(r)
		}
	}

	if currentToken.Len() > 0 {
		tokens = append(tokens, currentToken.String())
	}

	return tokens
}

func (p *Parser) ChunkText(text string, chunkSize, overlap int) []string {
	if chunkSize <= 0 {
		return []string{}
	}
	if overlap < 0 {
		overlap = 0
	}
	if overlap >= chunkSize {
		overlap = chunkSize - 1
	}

	tokens := p.Tokenize(text)
	if len(tokens) == 0 {
		return []string{}
	}

	chunks := make([]string, 0)
	start := 0

	for start < len(tokens) {
		end := start + chunkSize
		if end > len(tokens) {
			end = len(tokens)
		}

		chunk := strings.Join(tokens[start:end], " ")
		chunks = append(chunks, chunk)

		if end >= len(tokens) {
			break
		}

		start += chunkSize - overlap
	}

	return chunks
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
