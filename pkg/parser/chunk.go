package parser

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

type Parser struct{}

func NewParser() *Parser {
	return &Parser{}
}

func (p *Parser) Close() {
	// No cleanup needed without jieba
}

func (p *Parser) Tokenize(text string) []string {
	var tokens []string
	var currentToken strings.Builder

	for _, r := range text {
		if unicode.IsSpace(r) || unicode.IsPunct(r) {
			if currentToken.Len() > 0 {
				tokens = append(tokens, currentToken.String())
				currentToken.Reset()
			}
			continue
		}
		currentToken.WriteRune(r)
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

	segments := splitTextSegments(text)
	if len(segments) == 0 {
		return []string{}
	}

	chunks := make([]string, 0, len(segments))
	var current strings.Builder

	appendChunk := func(chunk string) {
		chunk = strings.TrimSpace(chunk)
		if chunk == "" {
			return
		}
		if overlap > 0 && len(chunks) > 0 {
			tail := tailRunes(chunks[len(chunks)-1], overlap)
			if tail != "" && !strings.HasPrefix(chunk, tail) {
				chunk = strings.TrimSpace(tail + "\n" + chunk)
			}
		}
		chunks = append(chunks, chunk)
	}

	for _, segment := range segments {
		segment = strings.TrimSpace(segment)
		if segment == "" {
			continue
		}

		if utf8.RuneCountInString(segment) > chunkSize {
			if current.Len() > 0 {
				appendChunk(current.String())
				current.Reset()
			}
			for _, part := range splitLongSegment(segment, chunkSize) {
				appendChunk(part)
			}
			continue
		}

		candidate := segment
		if current.Len() > 0 {
			candidate = current.String() + "\n" + segment
		}
		if utf8.RuneCountInString(candidate) <= chunkSize {
			current.Reset()
			current.WriteString(candidate)
			continue
		}

		appendChunk(current.String())
		current.Reset()
		current.WriteString(segment)
	}

	if current.Len() > 0 {
		appendChunk(current.String())
	}

	return chunks
}

func splitTextSegments(text string) []string {
	paragraphs := strings.Split(text, "\n")
	segments := make([]string, 0, len(paragraphs))
	for _, paragraph := range paragraphs {
		normalized := strings.Join(strings.Fields(strings.TrimSpace(paragraph)), " ")
		if normalized == "" {
			continue
		}
		segments = append(segments, splitParagraph(normalized)...)
	}
	return segments
}

func splitParagraph(paragraph string) []string {
	segments := make([]string, 0)
	var current strings.Builder
	flush := func() {
		segment := strings.TrimSpace(current.String())
		if segment != "" {
			segments = append(segments, segment)
		}
		current.Reset()
	}

	for _, r := range paragraph {
		current.WriteRune(r)
		if isSentenceBoundary(r) {
			flush()
		}
	}
	flush()
	return segments
}

func splitLongSegment(segment string, chunkSize int) []string {
	runes := []rune(segment)
	chunks := make([]string, 0, (len(runes)/chunkSize)+1)
	for start := 0; start < len(runes); start += chunkSize {
		end := start + chunkSize
		if end > len(runes) {
			end = len(runes)
		} else if end < len(runes) && isSentenceBoundary(runes[end]) {
			end++
		}
		part := strings.TrimSpace(string(runes[start:end]))
		if part != "" {
			chunks = append(chunks, part)
		}
	}
	return chunks
}

func tailRunes(text string, size int) string {
	if size <= 0 {
		return ""
	}
	runes := []rune(strings.TrimSpace(text))
	if len(runes) <= size {
		return string(runes)
	}
	return string(runes[len(runes)-size:])
}

func isSentenceBoundary(r rune) bool {
	switch r {
	case '。', '！', '？', '；', '.', '!', '?', ';':
		return true
	default:
		return false
	}
}
