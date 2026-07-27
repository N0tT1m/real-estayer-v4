package handler

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/realestayer/v4/internal/service"
)

// parseJSON caps request bodies at maxJSONBody. Before that, ~150 JSON routes
// each streamed an unbounded body straight into memory — ReadTimeout bounds how
// long a client may take to send, never how much it may send.

func TestParseJSONAcceptsANormalBody(t *testing.T) {
	h := &Handler{}
	var out struct {
		Destination string `json:"destination"`
	}
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"destination":"Lisbon"}`))
	if err := h.parseJSON(r, &out); err != nil {
		t.Fatalf("parseJSON: %v", err)
	}
	if out.Destination != "Lisbon" {
		t.Errorf("destination = %q, want Lisbon", out.Destination)
	}
}

func TestParseJSONRejectsAnOversizedBody(t *testing.T) {
	h := &Handler{}
	// Comfortably past the 1 MiB cap, and valid JSON throughout so the only
	// thing that can reject it is the size limit itself.
	huge := fmt.Sprintf(`{"destination":%q}`, strings.Repeat("A", 2<<20))
	var out struct {
		Destination string `json:"destination"`
	}
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(huge))
	err := h.parseJSON(r, &out)
	if err == nil {
		t.Fatal("parseJSON accepted a 2 MiB body; the MaxBytesReader cap is not applied")
	}
	var maxErr *http.MaxBytesError
	if !errors.As(err, &maxErr) {
		t.Errorf("error = %v; want a *http.MaxBytesError", err)
	}
}

// A body sitting just under the cap must still decode — an off-by-one here
// would break large-but-legitimate payloads like a refined itinerary.
func TestParseJSONAcceptsABodyJustUnderTheCap(t *testing.T) {
	h := &Handler{}
	padding := strings.Repeat("A", maxJSONBody-64)
	var out struct {
		Destination string `json:"destination"`
	}
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(
		fmt.Sprintf(`{"destination":%q}`, padding)))
	if err := h.parseJSON(r, &out); err != nil {
		t.Fatalf("body just under the cap was rejected: %v", err)
	}
	if len(out.Destination) != maxJSONBody-64 {
		t.Errorf("decoded %d bytes, want %d", len(out.Destination), maxJSONBody-64)
	}
}

// aiItineraryError is shared by both AI itinerary handlers so their status
// mapping cannot drift. A blank destination previously returned 502 Bad
// Gateway, blaming the upstream model for plainly bad input.
func TestAIItineraryErrorMapsStatuses(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{
			name: "unconfigured is 503",
			err:  service.ErrAIItineraryNotConfigured,
			want: http.StatusServiceUnavailable,
		},
		{
			name: "bad input is 400",
			err:  fmt.Errorf("%w: destination is required", service.ErrInvalidItineraryRequest),
			want: http.StatusBadRequest,
		},
		{
			name: "anything else is still 502",
			err:  errors.New("upstream returned nonsense"),
			want: http.StatusBadGateway,
		},
	}
	h := &Handler{}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h.aiItineraryError(rec, tc.err)
			if rec.Code != tc.want {
				t.Errorf("status = %d, want %d", rec.Code, tc.want)
			}
		})
	}
}
