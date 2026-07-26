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
