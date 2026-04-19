package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// PhotoStorage abstracts over "where do uploaded images live". Two
// implementations ship:
//
//   LocalPhotoStorage — writes to /app/data/uploads (configurable). Good for
//                       dev and single-box deployments.
//   S3PhotoStorage    — pushes to any S3-compatible bucket (AWS S3, R2,
//                       MinIO, Wasabi, etc.) using the AWS Signature V4
//                       algorithm over plain net/http, zero AWS-SDK bloat.
//
// Choice is driven by env: set UPLOADS_S3_BUCKET (+ keys) to opt into S3;
// otherwise the local driver is used. URLs returned are public — the caller
// must configure the bucket accordingly.
type PhotoStorage interface {
	// Save persists the image bytes and returns a public URL. contentType
	// may be empty; when it is, callers should ensure the data starts with
	// a MIME sniff-friendly signature (PNG, JPEG, etc.).
	Save(ctx context.Context, userID, filename string, reader io.Reader, contentType string) (string, error)
	// Kind returns a short string for logs ("local" / "s3").
	Kind() string
}

// NewPhotoStorage inspects env vars and constructs the appropriate backend.
// Always returns a non-nil storage — falls back to LocalPhotoStorage.
func NewPhotoStorage() PhotoStorage {
	if bucket := os.Getenv("UPLOADS_S3_BUCKET"); bucket != "" {
		return &S3PhotoStorage{
			Bucket:          bucket,
			Region:          getenvOr("UPLOADS_S3_REGION", "auto"),
			Endpoint:        os.Getenv("UPLOADS_S3_ENDPOINT"),
			AccessKey:       os.Getenv("UPLOADS_S3_ACCESS_KEY"),
			SecretKey:       os.Getenv("UPLOADS_S3_SECRET_KEY"),
			PublicBaseURL:   os.Getenv("UPLOADS_S3_PUBLIC_BASE_URL"),
			client:        &http.Client{Timeout: 30 * time.Second},
		}
	}
	dir := getenvOr("UPLOADS_DIR", "data/uploads")
	return &LocalPhotoStorage{Root: dir, PublicPrefix: "/uploads"}
}

// ---------- Local FS ----------

type LocalPhotoStorage struct {
	Root         string
	PublicPrefix string // URL prefix mapped by the HTTP server
}

func (s *LocalPhotoStorage) Kind() string { return "local" }

func (s *LocalPhotoStorage) Save(_ context.Context, userID, filename string, reader io.Reader, contentType string) (string, error) {
	if userID == "" {
		return "", errors.New("userID required")
	}
	safeName, ext := sanitizeFilename(filename, contentType)
	nonce, _ := randomHex(8)
	final := safeName + "-" + nonce + ext

	dir := filepath.Join(s.Root, sanitizePath(userID))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("mkdir: %w", err)
	}
	full := filepath.Join(dir, final)
	f, err := os.Create(full)
	if err != nil {
		return "", fmt.Errorf("create: %w", err)
	}
	defer f.Close()
	if _, err := io.Copy(f, io.LimitReader(reader, 10<<20)); err != nil { // 10 MB cap
		return "", fmt.Errorf("write: %w", err)
	}
	// Return as "<prefix>/<userID>/<filename>" — the handler mounts a
	// FileServer at PublicPrefix.
	return strings.TrimRight(s.PublicPrefix, "/") + "/" + url.PathEscape(userID) + "/" + url.PathEscape(final), nil
}

// ---------- S3-compatible ----------

// S3PhotoStorage uses plain net/http with a hand-rolled SigV4 to avoid pulling
// the full AWS SDK in. Supports R2, MinIO, Wasabi, Tigris, AWS S3 itself.
type S3PhotoStorage struct {
	Bucket        string
	Region        string
	Endpoint      string // e.g. "https://<account>.r2.cloudflarestorage.com"
	AccessKey     string
	SecretKey     string
	PublicBaseURL string // e.g. CDN hostname; falls back to Endpoint/Bucket
	client        *http.Client
}

func (s *S3PhotoStorage) Kind() string { return "s3" }

// Save PUTs the object and returns the public URL. Objects are stored under
// users/<userID>/<filename> for easy scoping.
func (s *S3PhotoStorage) Save(ctx context.Context, userID, filename string, reader io.Reader, contentType string) (string, error) {
	if s.AccessKey == "" || s.SecretKey == "" || s.Endpoint == "" {
		return "", errors.New("S3 storage not fully configured")
	}
	safeName, ext := sanitizeFilename(filename, contentType)
	nonce, _ := randomHex(8)
	key := fmt.Sprintf("users/%s/%s-%s%s", sanitizePath(userID), safeName, nonce, ext)

	body, err := io.ReadAll(io.LimitReader(reader, 10<<20))
	if err != nil {
		return "", err
	}
	if contentType == "" {
		contentType = http.DetectContentType(body)
	}
	putURL := fmt.Sprintf("%s/%s/%s", strings.TrimRight(s.Endpoint, "/"), url.PathEscape(s.Bucket), key)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, putURL, strings.NewReader(string(body)))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", contentType)
	req.ContentLength = int64(len(body))
	if err := signAWSv4(req, body, s.AccessKey, s.SecretKey, s.Region, "s3"); err != nil {
		return "", err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		msg, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("s3 PUT status %d: %s", resp.StatusCode, string(msg))
	}

	base := strings.TrimRight(s.PublicBaseURL, "/")
	if base == "" {
		base = fmt.Sprintf("%s/%s", strings.TrimRight(s.Endpoint, "/"), url.PathEscape(s.Bucket))
	}
	return base + "/" + key, nil
}

// ---------- Helpers ----------

func sanitizeFilename(name, contentType string) (base, ext string) {
	name = filepath.Base(name)
	ext = strings.ToLower(filepath.Ext(name))
	base = strings.TrimSuffix(name, ext)
	base = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			return r
		}
		return '-'
	}, base)
	if base == "" {
		base = "photo"
	}
	if ext == "" {
		if exts, _ := mime.ExtensionsByType(contentType); len(exts) > 0 {
			ext = exts[0]
		}
	}
	if ext == "" {
		ext = ".bin"
	}
	return base, ext
}

func sanitizePath(s string) string {
	s = strings.Trim(s, "./\\")
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			return r
		}
		return '-'
	}, s)
}

func getenvOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
