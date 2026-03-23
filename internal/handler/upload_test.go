package handler

import "testing"

func TestSanitizeUploadFilename(t *testing.T) {
	got := sanitizeUploadFilename(`../热词<>:"稿子".md`)
	if got != "热词____稿子_.md" {
		t.Fatalf("unexpected sanitized filename: %q", got)
	}
}

func TestIsAllowedUploadType(t *testing.T) {
	if !isAllowedUploadType(".md", "text/markdown") {
		t.Fatal("expected markdown text type to be allowed")
	}
	if isAllowedUploadType(".pdf", "text/plain") {
		t.Fatal("expected invalid pdf content type to be rejected")
	}
}

func TestPDFUploadRemainsMimeValidated(t *testing.T) {
	if !isAllowedUploadType(".pdf", "application/pdf") {
		t.Fatal("expected application/pdf to be allowed for pdf uploads")
	}
}
