package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ReceiptOCRService turns a photo of a receipt into a draft expense record
// via Claude's vision API. The output is deliberately narrow — only the
// fields we actually need to pre-fill an expense form.
type ReceiptOCRService struct {
	ai *AIItineraryService
	client *http.Client
}

var ErrReceiptOCRNotConfigured = errors.New("receipt OCR: ANTHROPIC_API_KEY not set")

func NewReceiptOCRService(ai *AIItineraryService) *ReceiptOCRService {
	return &ReceiptOCRService{ai: ai, client: &http.Client{Timeout: 30 * time.Second}}
}

func (s *ReceiptOCRService) Configured() bool { return s.ai != nil && s.ai.Configured() }

// ReceiptExtract is what we return to the handler.
type ReceiptExtract struct {
	Merchant string    `json:"merchant,omitempty"`
	Total    float64   `json:"total,omitempty"`
	Currency string    `json:"currency,omitempty"`
	Category string    `json:"category,omitempty"` // flight/hotel/car/activity/other
	Date     time.Time `json:"date,omitempty"`
	Notes    string    `json:"notes,omitempty"`
}

// FromImageURL runs OCR against a publicly-reachable URL.
func (s *ReceiptOCRService) FromImageURL(ctx context.Context, imageURL string) (*ReceiptExtract, error) {
	if !s.Configured() {
		return nil, ErrReceiptOCRNotConfigured
	}
	if strings.TrimSpace(imageURL) == "" {
		return nil, errors.New("image URL required")
	}
	return s.run(ctx, map[string]any{
		"type":   "image",
		"source": map[string]string{"type": "url", "url": imageURL},
	})
}

// FromImageBytes runs OCR against raw bytes (e.g. a multipart upload). mime
// should be "image/jpeg" or "image/png" — defaults to jpeg.
func (s *ReceiptOCRService) FromImageBytes(ctx context.Context, data io.Reader, mime string) (*ReceiptExtract, error) {
	if !s.Configured() {
		return nil, ErrReceiptOCRNotConfigured
	}
	buf, err := io.ReadAll(io.LimitReader(data, 6<<20)) // 6 MB cap for Anthropic
	if err != nil {
		return nil, err
	}
	if mime == "" {
		mime = "image/jpeg"
	}
	return s.run(ctx, map[string]any{
		"type": "image",
		"source": map[string]string{
			"type":       "base64",
			"media_type": mime,
			"data":       base64.StdEncoding.EncodeToString(buf),
		},
	})
}

// run sends the image block alongside a tight system prompt and parses the
// JSON response. Any parse failure returns a wrapped error.
func (s *ReceiptOCRService) run(ctx context.Context, imagePart map[string]any) (*ReceiptExtract, error) {
	system := `You are a receipt-reader. Respond ONLY with a single JSON object matching:
{"merchant":"<name>","total":<number>,"currency":"<ISO 4217>","category":"<flight|hotel|car|activity|food|other>","date":"<YYYY-MM-DD>","notes":"<short free text>"}
Use null / empty strings if you're unsure. Do not add markdown.`

	body := map[string]any{
		"model":      s.ai.model,
		"max_tokens": 500,
		"system": []map[string]any{{
			"type": "text", "text": system,
			"cache_control": map[string]string{"type": "ephemeral"},
		}},
		"messages": []map[string]any{{
			"role": "user",
			"content": []any{
				imagePart,
				map[string]string{"type": "text", "text": "Extract the receipt fields as JSON."},
			},
		}},
	}
	buf, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.anthropic.com/v1/messages", bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", s.ai.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("anthropic: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("anthropic: status %d", resp.StatusCode)
	}

	var apiResp struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, err
	}
	raw := ""
	for _, c := range apiResp.Content {
		if c.Type == "text" {
			raw += c.Text
		}
	}
	raw = stripCodeFence(strings.TrimSpace(raw))

	var wire struct {
		Merchant string  `json:"merchant"`
		Total    float64 `json:"total"`
		Currency string  `json:"currency"`
		Category string  `json:"category"`
		Date     string  `json:"date"`
		Notes    string  `json:"notes"`
	}
	if err := json.Unmarshal([]byte(raw), &wire); err != nil {
		return nil, fmt.Errorf("parse OCR response: %w (raw: %.200s)", err, raw)
	}
	out := &ReceiptExtract{
		Merchant: wire.Merchant, Total: wire.Total, Currency: strings.ToUpper(wire.Currency),
		Category: wire.Category, Notes: wire.Notes,
	}
	if t, err := time.Parse("2006-01-02", wire.Date); err == nil {
		out.Date = t
	}
	return out, nil
}
