package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/five07/gt-order-fsm/internal/fulfillment"
	"github.com/five07/gt-order-fsm/internal/order"
	"github.com/five07/gt-order-fsm/internal/payment"
	"github.com/five07/gt-order-fsm/internal/store"
)

const validBody = `{"amount_cents": 12500, "currency": "USD", "card_number": "4242424242424242"}`

func newTestHandler(pay *payment.Stub, ful *fulfillment.Stub) *Handler {
	log := slog.New(slog.DiscardHandler)
	svc := order.NewService(store.NewMemory(), pay, ful, log, time.Now)
	return New(svc, log)
}

// do sends a request and decodes the JSON response into out, if out is not nil.
func do(t *testing.T, h http.Handler, method, path, body string, out any) int {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("%s %s Content-Type = %q, want application/json", method, path, got)
	}
	if out != nil {
		if err := json.NewDecoder(rec.Body).Decode(out); err != nil {
			t.Fatalf("%s %s: decode response: %v", method, path, err)
		}
	}
	return rec.Code
}

func TestOrderLifecycle(t *testing.T) {
	errFulfill := errors.New("ticketing unavailable")
	errVoid := errors.New("gateway timeout")

	tests := []struct {
		name         string
		authorizeErr error
		completeErr  error
		voidErr      error

		wantStates     []order.State // after each advance
		wantLastErrors int           // errors on the final history entry
	}{
		{
			name:       "happy path",
			wantStates: []order.State{order.StatePaymentAuthorized, order.StateComplete},
		},
		{
			name:           "payment declined",
			authorizeErr:   fmt.Errorf("card refused: %w", payment.ErrDeclined),
			wantStates:     []order.State{order.StateRejected},
			wantLastErrors: 1,
		},
		{
			name:           "fulfillment fails, void succeeds",
			completeErr:    errFulfill,
			wantStates:     []order.State{order.StatePaymentAuthorized, order.StateCancelled},
			wantLastErrors: 1,
		},
		{
			name:           "fulfillment fails, void fails",
			completeErr:    errFulfill,
			voidErr:        errVoid,
			wantStates:     []order.State{order.StatePaymentAuthorized, order.StateNeedsAttention},
			wantLastErrors: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newTestHandler(
				&payment.Stub{AuthorizeErr: tt.authorizeErr, VoidErr: tt.voidErr},
				&fulfillment.Stub{Err: tt.completeErr},
			)

			var created orderResponse
			if code := do(t, h, "POST", "/orders", validBody, &created); code != http.StatusCreated {
				t.Fatalf("create status = %d, want %d", code, http.StatusCreated)
			}
			if created.State != order.StateInitialized || created.AmountCents != 12500 || created.Currency != "USD" {
				t.Fatalf("created = %+v, want initialized 12500 USD", created)
			}

			path := "/orders/" + created.ID
			for i, want := range tt.wantStates {
				var got orderResponse
				if code := do(t, h, "POST", path+"/advance", "", &got); code != http.StatusOK {
					t.Fatalf("advance #%d status = %d, want %d", i+1, code, http.StatusOK)
				}
				if got.State != want {
					t.Fatalf("advance #%d state = %s, want %s", i+1, got.State, want)
				}
			}

			var got orderResponse
			if code := do(t, h, "GET", path, "", &got); code != http.StatusOK {
				t.Fatalf("get status = %d, want %d", code, http.StatusOK)
			}
			if len(got.History) != len(tt.wantStates)+1 {
				t.Fatalf("history has %d entries, want %d", len(got.History), len(tt.wantStates)+1)
			}
			last := got.History[len(got.History)-1]
			if len(last.Errors) != tt.wantLastErrors {
				t.Errorf("last history errors = %q, want %d of them", last.Errors, tt.wantLastErrors)
			}
		})
	}
}

func TestErrorStatuses(t *testing.T) {
	t.Run("malformed create bodies are 400", func(t *testing.T) {
		h := newTestHandler(&payment.Stub{}, &fulfillment.Stub{})
		bodies := map[string]string{
			"not JSON":       `{`,
			"unknown field":  `{"amount_cents": 100, "currency": "USD", "tip": 5}`,
			"zero amount":    `{"amount_cents": 0, "currency": "USD"}`,
			"bad currency":   `{"amount_cents": 100, "currency": "DOLLARS"}`,
			"missing fields": `{}`,
			"missing card":   `{"amount_cents": 100, "currency": "USD"}`,
			"blank card":     `{"amount_cents": 100, "currency": "USD", "card_number": "  "}`,
		}
		for name, body := range bodies {
			var resp errorResponse
			if code := do(t, h, "POST", "/orders", body, &resp); code != http.StatusBadRequest {
				t.Errorf("%s: status = %d, want %d", name, code, http.StatusBadRequest)
			}
			if resp.Error == "" {
				t.Errorf("%s: empty error message", name)
			}
		}
	})

	t.Run("unknown order is 404", func(t *testing.T) {
		h := newTestHandler(&payment.Stub{}, &fulfillment.Stub{})
		if code := do(t, h, "GET", "/orders/missing", "", nil); code != http.StatusNotFound {
			t.Errorf("get status = %d, want %d", code, http.StatusNotFound)
		}
		if code := do(t, h, "POST", "/orders/missing/advance", "", nil); code != http.StatusNotFound {
			t.Errorf("advance status = %d, want %d", code, http.StatusNotFound)
		}
	})

	t.Run("advancing a terminal order is 409", func(t *testing.T) {
		h := newTestHandler(&payment.Stub{AuthorizeErr: payment.ErrDeclined}, &fulfillment.Stub{})
		var created orderResponse
		do(t, h, "POST", "/orders", validBody, &created)
		do(t, h, "POST", "/orders/"+created.ID+"/advance", "", nil)

		if code := do(t, h, "POST", "/orders/"+created.ID+"/advance", "", nil); code != http.StatusConflict {
			t.Errorf("status = %d, want %d", code, http.StatusConflict)
		}
	})

	t.Run("transient gateway error is 502 and leaves the order unchanged", func(t *testing.T) {
		h := newTestHandler(&payment.Stub{AuthorizeErr: errors.New("connection reset")}, &fulfillment.Stub{})
		var created orderResponse
		do(t, h, "POST", "/orders", validBody, &created)

		var resp errorResponse
		if code := do(t, h, "POST", "/orders/"+created.ID+"/advance", "", &resp); code != http.StatusBadGateway {
			t.Errorf("status = %d, want %d", code, http.StatusBadGateway)
		}
		if strings.Contains(resp.Error, "connection reset") {
			t.Errorf("502 body leaks the upstream error: %q", resp.Error)
		}

		var got orderResponse
		do(t, h, "GET", "/orders/"+created.ID, "", &got)
		if got.State != order.StateInitialized || len(got.History) != 1 {
			t.Errorf("order is %s with %d entries, want unchanged", got.State, len(got.History))
		}
	})
}

// failingStore is a Memory store whose Save always fails.
type failingStore struct{ *store.Memory }

func (failingStore) Save(context.Context, *order.Order) error {
	return errors.New("disk full")
}

func TestInternalErrorHidesDetails(t *testing.T) {
	log := slog.New(slog.DiscardHandler)
	svc := order.NewService(failingStore{store.NewMemory()}, &payment.Stub{}, &fulfillment.Stub{}, log, time.Now)
	h := New(svc, log)

	var resp errorResponse
	if code := do(t, h, "POST", "/orders", validBody, &resp); code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", code, http.StatusInternalServerError)
	}
	if resp.Error != "internal error" {
		t.Errorf("error = %q, want %q", resp.Error, "internal error")
	}
}

func TestErrorStrings(t *testing.T) {
	a, b := errors.New("a"), errors.New("b")
	tests := []struct {
		name string
		err  error
		want []string
	}{
		{"nil", nil, nil},
		{"plain", a, []string{"a"}},
		{"wrapped", fmt.Errorf("ctx: %w", a), []string{"ctx: a"}},
		{"joined", errors.Join(a, b), []string{"a", "b"}},
	}
	for _, tt := range tests {
		if got := errorStrings(tt.err); !slices.Equal(got, tt.want) {
			t.Errorf("%s: errorStrings = %q, want %q", tt.name, got, tt.want)
		}
	}
}

// TestTestCards drives each outcome over HTTP with the default stubs, the way
// a reviewer would with curl.
func TestTestCards(t *testing.T) {
	// %s in a note is replaced with the order's authorization ID.
	tests := []struct {
		card       string
		wantFinal  order.State
		wantNote   string
		wantErrors []string
	}{
		{
			card:      "4242424242424242",
			wantFinal: order.StateComplete,
			wantNote:  "order fulfilled",
		},
		{
			card:       payment.TestCardDecline,
			wantFinal:  order.StateRejected,
			wantNote:   "payment declined; nothing held",
			wantErrors: []string{"payment declined: simulated by test card 9999"},
		},
		{
			card:       payment.TestCardFulfillmentFails,
			wantFinal:  order.StateCancelled,
			wantNote:   "fulfillment failed; authorization %s voided",
			wantErrors: []string{"fulfillment failed: simulated by test card 9998"},
		},
		{
			card:      payment.TestCardVoidFails,
			wantFinal: order.StateNeedsAttention,
			wantNote:  "fulfillment failed; void of authorization %s failed, funds may still be held",
			wantErrors: []string{
				"fulfillment failed: simulated by test card 9997",
				"void failed: simulated by test card 9997",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.card, func(t *testing.T) {
			h := newTestHandler(&payment.Stub{}, &fulfillment.Stub{})
			body := fmt.Sprintf(`{"amount_cents": 12500, "currency": "USD", "card_number": %q}`, tt.card)

			var got orderResponse
			if code := do(t, h, "POST", "/orders", body, &got); code != http.StatusCreated {
				t.Fatalf("create status = %d, want %d", code, http.StatusCreated)
			}
			for !got.State.IsTerminal() {
				if code := do(t, h, "POST", "/orders/"+got.ID+"/advance", "", &got); code != http.StatusOK {
					t.Fatalf("advance status = %d, want %d", code, http.StatusOK)
				}
			}

			if got.State != tt.wantFinal {
				t.Errorf("final state = %s, want %s", got.State, tt.wantFinal)
			}
			last := got.History[len(got.History)-1]
			wantNote := tt.wantNote
			if strings.Contains(wantNote, "%s") {
				wantNote = fmt.Sprintf(wantNote, got.AuthorizationID)
			}
			if last.Note != wantNote {
				t.Errorf("last note = %q, want %q", last.Note, wantNote)
			}
			if !slices.Equal(last.Errors, tt.wantErrors) {
				t.Errorf("last errors = %q, want %q", last.Errors, tt.wantErrors)
			}
		})
	}
}

func TestCardNumberIsMasked(t *testing.T) {
	h := newTestHandler(&payment.Stub{}, &fulfillment.Stub{})
	req := httptest.NewRequest("POST", "/orders", strings.NewReader(validBody))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if strings.Contains(rec.Body.String(), "4242424242424242") {
		t.Fatalf("response contains the full card number: %s", rec.Body)
	}
	var got orderResponse
	json.NewDecoder(rec.Body).Decode(&got)
	if got.CardLast4 != "4242" {
		t.Errorf("card_last4 = %q, want %q", got.CardLast4, "4242")
	}
}
