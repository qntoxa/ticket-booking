package domain

import (
	"errors"
	"time"
)

type Role string

const (
	RoleUser  Role = "USER"
	RoleAdmin Role = "ADMIN"
)

type SeatStatus string

const (
	SeatAvailable SeatStatus = "AVAILABLE"
	SeatHeld      SeatStatus = "HELD"
	SeatSold      SeatStatus = "SOLD"
)

var (
	ErrSeatNotFound       = errors.New("seat not found")
	ErrSeatAlreadyTaken   = errors.New("seat is already held or sold")
	ErrLockBusy           = errors.New("seat is currently locked by a concurrent request")
	ErrHoldNotFound       = errors.New("hold reservation not found")
	ErrHoldExpired        = errors.New("hold reservation has expired")
	ErrHoldOwnership      = errors.New("hold belongs to another user")
	ErrUnauthorized       = errors.New("unauthorized")
	ErrForbidden          = errors.New("forbidden: insufficient role permissions")
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrUserAlreadyExists  = errors.New("user with this email already exists")
	ErrMissingIdempotency = errors.New("Idempotency-Key header is required")
)

type User struct {
	ID           string    `json:"id"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"-"`
	Role         Role      `json:"role"`
	CreatedAt    time.Time `json:"created_at"`
}

type Event struct {
	ID         string    `json:"id"`
	Title      string    `json:"title"`
	Venue      string    `json:"venue"`
	StartsAt   time.Time `json:"starts_at"`
	TotalSeats int       `json:"total_seats"`
	CreatedAt  time.Time `json:"created_at"`
}

type Seat struct {
	ID            string     `json:"id"`
	EventID       string     `json:"event_id"`
	SeatNumber    string     `json:"seat_number"`
	PriceCents    int64      `json:"price_cents"`
	Status        SeatStatus `json:"status"`
	HeldByUserID  string     `json:"held_by_user_id,omitempty"`
	HoldID        string     `json:"hold_id,omitempty"`
	HoldExpiresAt *time.Time `json:"hold_expires_at,omitempty"`
	Version       int64      `json:"version"`
}

type HoldReservation struct {
	HoldID      string    `json:"hold_id"`
	EventID     string    `json:"event_id"`
	SeatID      string    `json:"seat_id"`
	SeatNumber  string    `json:"seat_number"`
	UserID      string    `json:"user_id"`
	PriceCents  int64     `json:"price_cents"`
	ExpiresAt   time.Time `json:"expires_at"`
	SeatVersion int64     `json:"seat_version"`
}

type Order struct {
	ID             string    `json:"id"`
	HoldID         string    `json:"hold_id"`
	UserID         string    `json:"user_id"`
	EventID        string    `json:"event_id"`
	SeatID         string    `json:"seat_id"`
	SeatNumber     string    `json:"seat_number"`
	AmountCents    int64     `json:"amount_cents"`
	IdempotencyKey string    `json:"idempotency_key"`
	Status         string    `json:"status"`
	CreatedAt      time.Time `json:"created_at"`
}
