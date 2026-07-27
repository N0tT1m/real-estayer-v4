package service

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
)

func TestLocalPhotoStorageSaveAndURL(t *testing.T) {
	dir, err := os.MkdirTemp("", "re-uploads-")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	s := &LocalPhotoStorage{Root: dir, PublicPrefix: "/uploads"}
	url, err := s.Save(context.Background(), "user123", "holiday.jpg", bytes.NewReader([]byte("fake-jpeg-bytes")), "image/jpeg")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(url, "/uploads/user123/") || !strings.HasSuffix(url, ".jpg") {
		t.Errorf("unexpected URL: %s", url)
	}
	if s.Kind() != "local" {
		t.Errorf("Kind = %s", s.Kind())
	}
}

func TestLocalPhotoStorageRejectsEmptyUser(t *testing.T) {
	dir, _ := os.MkdirTemp("", "re-uploads-")
	defer func() { _ = os.RemoveAll(dir) }()
	s := &LocalPhotoStorage{Root: dir, PublicPrefix: "/uploads"}
	if _, err := s.Save(context.Background(), "", "x.png", bytes.NewReader([]byte("x")), "image/png"); err == nil {
		t.Errorf("expected error for empty userID")
	}
}

func TestSanitizeFilename(t *testing.T) {
	base, ext := sanitizeFilename("my photo!!.jpg", "image/jpeg")
	if base == "" || strings.ContainsAny(base, "! ") {
		t.Errorf("sanitize left bad chars: %q", base)
	}
	if ext != ".jpg" {
		t.Errorf("ext = %q; want .jpg", ext)
	}
	_, ext2 := sanitizeFilename("nope", "image/png")
	if ext2 != ".png" {
		t.Errorf("ext from MIME = %q; want .png", ext2)
	}
}

// The upload handler rejects non-images by magic bytes but accepts the client's
// *filename*, so a genuine PNG called "x.html" would be stored as .html and
// served back from /uploads as text/html. A PNG that is also valid HTML is a
// well-known polyglot, which turns a photo upload into stored XSS on our own
// origin. The extension has to be derived, never trusted.
func TestSanitizeFilenameRejectsADangerousExtension(t *testing.T) {
	cases := []struct {
		name        string
		filename    string
		contentType string
	}{
		{"html polyglot", "polyglot.html", "image/png"},
		{"svg carries script", "drawing.svg", "image/png"},
		{"server-side script", "shell.php", "image/jpeg"},
		{"uppercase is not a bypass", "POLYGLOT.HTML", "image/png"},
		{"double extension", "photo.png.html", "image/png"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, ext := sanitizeFilename(tc.filename, tc.contentType)
			if ext == ".html" || ext == ".htm" || ext == ".svg" || ext == ".php" {
				t.Fatalf("kept the client's extension %q for %q", ext, tc.filename)
			}
			if !imageExtensions[ext] && ext != ".bin" {
				t.Errorf("ext = %q; want an image extension or the .bin fallback", ext)
			}
		})
	}
}

// Dropping a bad extension must not mean dropping the real one: an ordinary
// upload has to keep the extension that matches its bytes.
func TestSanitizeFilenameKeepsGenuineImageExtensions(t *testing.T) {
	cases := map[string]string{
		"holiday.jpg":  ".jpg",
		"holiday.jpeg": ".jpeg",
		"holiday.PNG":  ".png",
		"holiday.webp": ".webp",
		"holiday.gif":  ".gif",
	}
	for filename, want := range cases {
		if _, ext := sanitizeFilename(filename, "image/jpeg"); ext != want {
			t.Errorf("sanitizeFilename(%q) ext = %q, want %q", filename, ext, want)
		}
	}
}

// When neither the filename nor the sniffed type yields an image extension,
// the result must be .bin — served as application/octet-stream, it downloads
// rather than executes, which is the safe way to fail.
func TestSanitizeFilenameFallsBackToBin(t *testing.T) {
	if _, ext := sanitizeFilename("mystery.html", "application/octet-stream"); ext != ".bin" {
		t.Errorf("ext = %q, want .bin", ext)
	}
}

// DetectContentType may append "; charset=utf-8"; mime.ExtensionsByType wants
// the bare type, so the parameter has to be stripped before the lookup.
func TestMimeExtensionsIgnoresParameters(t *testing.T) {
	exts := mimeExtensions("image/png; charset=binary")
	found := false
	for _, e := range exts {
		if strings.EqualFold(e, ".png") {
			found = true
		}
	}
	if !found {
		t.Errorf("mimeExtensions did not resolve a parameterised type: %v", exts)
	}
}

func TestSanitizePath(t *testing.T) {
	if sanitizePath("../../etc/passwd") == "../../etc/passwd" {
		t.Errorf("sanitizePath should strip path separators")
	}
	if sanitizePath("user 123") == "user 123" {
		t.Errorf("spaces should be escaped")
	}
}

func TestNewPhotoStorageDefaultsLocal(t *testing.T) {
	t.Setenv("UPLOADS_S3_BUCKET", "")
	s := NewPhotoStorage()
	if s.Kind() != "local" {
		t.Errorf("default should be local, got %s", s.Kind())
	}
}

func TestNewPhotoStorageS3WhenBucketSet(t *testing.T) {
	t.Setenv("UPLOADS_S3_BUCKET", "my-bucket")
	s := NewPhotoStorage()
	if s.Kind() != "s3" {
		t.Errorf("should pick s3 when bucket set, got %s", s.Kind())
	}
}
