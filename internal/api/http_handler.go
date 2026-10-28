package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/user/ticket-booking/internal/auth"
	"github.com/user/ticket-booking/internal/domain"
	"github.com/user/ticket-booking/internal/service"
)

type ctxKey string

const claimsCtxKey ctxKey = "jwt_claims"

type Handler struct {
	svc        *service.BookingService
	jwtManager *auth.Manager
}

func NewHandler(svc *service.BookingService, jwtManager *auth.Manager) *Handler {
	return &Handler{
		svc:        svc,
		jwtManager: jwtManager,
	}
}

func (h *Handler) Router() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", h.handleHealth)
	mux.HandleFunc("GET /swagger/openapi.yaml", h.handleOpenAPISpec)

	mux.HandleFunc("POST /api/v1/auth/register", h.handleRegister)
	mux.HandleFunc("POST /api/v1/auth/login", h.handleLogin)
	mux.HandleFunc("POST /api/v1/auth/refresh", h.handleRefresh)

	mux.HandleFunc("GET /api/v1/events", h.handleListEvents)
	mux.HandleFunc("GET /api/v1/events/{id}/seats", h.handleListSeats)

	mux.Handle("POST /api/v1/bookings/hold", h.requireAuth(http.HandlerFunc(h.handleHoldSeat)))
	mux.Handle("POST /api/v1/bookings/confirm", h.requireAuth(http.HandlerFunc(h.handleConfirmBooking)))

	mux.Handle("POST /api/v1/events", h.requireAuth(h.requireRole(domain.RoleAdmin, http.HandlerFunc(h.handleCreateEvent))))

	return mux
}

func (h *Handler) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		Header := r.Header.Get("Authorization")
		if !strings.HasPrefix(Header, "Bearer ") {
			writeError(w, http.StatusUnauthorized, "missing or invalid Authorization Bearer header")
			return
		}
		token := strings.TrimPrefix(Header, "Bearer ")
		claims, err := h.jwtManager.Verify(token, auth.TokenAccess)
		if err != nil {
			writeError(w, http.StatusUnauthorized, err.Error())
			return
		}
		ctx := context.WithValue(r.Context(), claimsCtxKey, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (h *Handler) requireRole(role domain.Role, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := r.Context().Value(claimsCtxKey).(auth.Claims)
		if !ok || claims.Role != role {
			writeError(w, http.StatusForbidden, domain.ErrForbidden.Error())
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (h *Handler) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":    "ok",
		"timestamp": time.Now().UTC(),
	})
}

func (h *Handler) handleOpenAPISpec(w http.ResponseWriter, r *http.Request) {
	data, err := os.ReadFile("docs/openapi.yaml")
	if err != nil {
		writeError(w, http.StatusNotFound, "openapi spec not found")
		return
	}
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (h *Handler) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string      `json:"email"`
		Password string      `json:"password"`
		Role     domain.Role `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	user, access, refresh, err := h.svc.RegisterUser(req.Email, req.Password, req.Role)
	if err != nil {
		if errors.Is(err, domain.ErrUserAlreadyExists) {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"user":          user,
		"access_token":  access,
		"refresh_token": refresh,
	})
}

func (h *Handler) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	user, access, refresh, err := h.svc.Login(req.Email, req.Password)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"user":          user,
		"access_token":  access,
		"refresh_token": refresh,
	})
}

func (h *Handler) handleRefresh(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	access, refresh, err := h.svc.RefreshTokens(req.RefreshToken)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token":  access,
		"refresh_token": refresh,
	})
}

func (h *Handler) handleListEvents(w http.ResponseWriter, r *http.Request) {
	events := h.svc.ListEvents()
	writeJSON(w, http.StatusOK, map[string]any{
		"items": events,
		"count": len(events),
	})
}

func (h *Handler) handleCreateEvent(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title       string `json:"title"`
		Venue       string `json:"venue"`
		Rows        int    `json:"rows"`
		SeatsPerRow int    `json:"seats_per_row"`
		PriceCents  int64  `json:"price_cents"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if req.Rows <= 0 {
		req.Rows = 4
	}
	if req.SeatsPerRow <= 0 {
		req.SeatsPerRow = 10
	}
	if req.PriceCents <= 0 {
		req.PriceCents = 5000
	}
	evt := h.svc.CreateEvent(req.Title, req.Venue, time.Now().UTC().Add(7*24*time.Hour), req.Rows, req.SeatsPerRow, req.PriceCents)
	writeJSON(w, http.StatusCreated, evt)
}

func (h *Handler) handleListSeats(w http.ResponseWriter, r *http.Request) {
	eventID := r.PathValue("id")
	seats, err := h.svc.ListSeats(eventID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"event_id": eventID,
		"seats":    seats,
		"count":    len(seats),
	})
}

func (h *Handler) handleHoldSeat(w http.ResponseWriter, r *http.Request) {
	claims := r.Context().Value(claimsCtxKey).(auth.Claims)
	var req struct {
		EventID    string `json:"event_id"`
		SeatNumber string `json:"seat_number"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}

	hold, err := h.svc.HoldSeat(r.Context(), claims.UserID, req.EventID, req.SeatNumber)
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrSeatNotFound):
			writeError(w, http.StatusNotFound, err.Error())
		case errors.Is(err, domain.ErrSeatAlreadyTaken), errors.Is(err, domain.ErrLockBusy):
			writeError(w, http.StatusConflict, err.Error())
		default:
			writeError(w, http.StatusBadRequest, err.Error())
		}
		return
	}

	writeJSON(w, http.StatusCreated, hold)
}

func (h *Handler) handleConfirmBooking(w http.ResponseWriter, r *http.Request) {
	claims := r.Context().Value(claimsCtxKey).(auth.Claims)
	idempKey := r.Header.Get("Idempotency-Key")

	var req struct {
		HoldID string `json:"hold_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}

	order, replayed, err := h.svc.ConfirmBooking(r.Context(), claims.UserID, req.HoldID, idempKey)
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrMissingIdempotency):
			writeError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, domain.ErrHoldNotFound):
			writeError(w, http.StatusNotFound, err.Error())
		case errors.Is(err, domain.ErrHoldOwnership):
			writeError(w, http.StatusForbidden, err.Error())
		case errors.Is(err, domain.ErrHoldExpired), errors.Is(err, domain.ErrSeatAlreadyTaken):
			writeError(w, http.StatusConflict, err.Error())
		default:
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}

	if replayed {
		w.Header().Set("X-Idempotent-Replay", "true")
	}
	writeJSON(w, http.StatusOK, order)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
