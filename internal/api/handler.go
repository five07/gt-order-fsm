package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/five07/gt-order-fsm/internal/order"
	"github.com/five07/gt-order-fsm/internal/payment"
	"golang.org/x/text/currency"
)

// Handler serves the order HTTP API.
type Handler struct {
	svc *order.Service
	log *slog.Logger
	mux *http.ServeMux
}

// New returns a Handler with its routes registered.
func New(svc *order.Service, log *slog.Logger) *Handler {
	h := &Handler{svc: svc, log: log, mux: http.NewServeMux()}
	h.mux.HandleFunc("POST /orders", h.create)
	h.mux.HandleFunc("GET /orders/{id}", h.get)
	h.mux.HandleFunc("POST /orders/{id}/advance", h.advance)
	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mux.ServeHTTP(w, r)
}

// create a new order
func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	req, err := validateCreateRequest(w, r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	o, err := h.svc.Create(r.Context(), req.amount, req.card)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, newOrderResponse(o))
}

// get an existing order
func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	o, err := h.svc.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, newOrderResponse(o))
}

// advance an order to the next step
func (h *Handler) advance(w http.ResponseWriter, r *http.Request) {
	o, err := h.svc.Advance(r.Context(), r.PathValue("id"))
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, newOrderResponse(o))
}

// createInput is a validated create request.
type createInput struct {
	amount payment.Money
	card   string
}

// validateCreateRequest validates a create request
func validateCreateRequest(w http.ResponseWriter, r *http.Request) (createInput, error) {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()

	var req createOrderRequest
	if err := dec.Decode(&req); err != nil {
		return createInput{}, fmt.Errorf("invalid JSON body: %w", err)
	}
	if req.AmountCents <= 0 {
		return createInput{}, errors.New("amount_cents must be greater than zero")
	}
	cur, err := currency.ParseISO(req.Currency)
	if err != nil {
		return createInput{}, fmt.Errorf("currency %q is not an ISO currency code", req.Currency)
	}
	card := strings.TrimSpace(req.CardNumber)
	if card == "" {
		return createInput{}, errors.New("card_number is required")
	}
	return createInput{
		amount: payment.Money{Amount: req.AmountCents, Currency: cur},
		card:   card,
	}, nil
}

// writeServiceError maps service errors to HTTP statuses.
func (h *Handler) writeServiceError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, order.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, order.ErrTerminalState),
		errors.Is(err, order.ErrInvalidTransition),
		errors.Is(err, order.ErrAdvanceInProgress):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, order.ErrUpstream):
		h.log.Warn("upstream failure", "method", r.Method, "path", r.URL.Path, "error", err)
		writeError(w, http.StatusBadGateway, "upstream failure; the order is unchanged and the request can be retried")
	default:
		h.log.Error("request failed", "method", r.Method, "path", r.URL.Path, "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorResponse{Error: msg})
}
