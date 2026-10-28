package service

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/user/ticket-booking/internal/auth"
	"github.com/user/ticket-booking/internal/domain"
	"github.com/user/ticket-booking/internal/lock"
)

type BookingService struct {
	mu             sync.RWMutex
	usersByEmail   map[string]domain.User
	usersByID      map[string]domain.User
	events         map[string]domain.Event
	seatsByEvent   map[string]map[string]*domain.Seat
	holds          map[string]domain.HoldReservation
	ordersByIdemp  map[string]domain.Order
	ordersByID     map[string]domain.Order

	locker         lock.DistributedLocker
	jwtManager     *auth.Manager
	holdTTL        time.Duration
	seq            atomic.Uint64
	expiredCleaned atomic.Uint64
}

func NewBookingService(locker lock.DistributedLocker, jwtManager *auth.Manager, holdTTL time.Duration) *BookingService {
	return &BookingService{
		usersByEmail:  make(map[string]domain.User),
		usersByID:     make(map[string]domain.User),
		events:        make(map[string]domain.Event),
		seatsByEvent:  make(map[string]map[string]*domain.Seat),
		holds:         make(map[string]domain.HoldReservation),
		ordersByIdemp: make(map[string]domain.Order),
		ordersByID:    make(map[string]domain.Order),
		locker:        locker,
		jwtManager:    jwtManager,
		holdTTL:       holdTTL,
	}
}

func (s *BookingService) RegisterUser(email, password string, role domain.Role) (domain.User, string, string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" || len(password) < 4 {
		return domain.User{}, "", "", domain.ErrInvalidCredentials
	}
	if role != domain.RoleAdmin {
		role = domain.RoleUser
	}

	s.mu.Lock()
	if _, exists := s.usersByEmail[email]; exists {
		s.mu.Unlock()
		return domain.User{}, "", "", domain.ErrUserAlreadyExists
	}

	user := domain.User{
		ID:           fmt.Sprintf("usr-%d", s.seq.Add(1)),
		Email:        email,
		PasswordHash: auth.HashPassword(password),
		Role:         role,
		CreatedAt:    time.Now().UTC(),
	}
	s.usersByEmail[email] = user
	s.usersByID[user.ID] = user
	s.mu.Unlock()

	access, refresh, err := s.jwtManager.IssuePair(user)
	return user, access, refresh, err
}

func (s *BookingService) Login(email, password string) (domain.User, string, string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	hash := auth.HashPassword(password)

	s.mu.RLock()
	user, exists := s.usersByEmail[email]
	s.mu.RUnlock()

	if !exists || user.PasswordHash != hash {
		return domain.User{}, "", "", domain.ErrInvalidCredentials
	}

	access, refresh, err := s.jwtManager.IssuePair(user)
	return user, access, refresh, err
}

func (s *BookingService) RefreshTokens(refreshToken string) (string, string, error) {
	claims, err := s.jwtManager.Verify(refreshToken, auth.TokenRefresh)
	if err != nil {
		return "", "", domain.ErrUnauthorized
	}

	s.mu.RLock()
	user, exists := s.usersByID[claims.UserID]
	s.mu.RUnlock()
	if !exists {
		return "", "", domain.ErrUnauthorized
	}

	return s.jwtManager.IssuePair(user)
}

func (s *BookingService) CreateEvent(title, venue string, startsAt time.Time, rows int, seatsPerRow int, priceCents int64) domain.Event {
	s.mu.Lock()
	defer s.mu.Unlock()

	total := rows * seatsPerRow
	evt := domain.Event{
		ID:         fmt.Sprintf("evt-%d", s.seq.Add(1)),
		Title:      title,
		Venue:      venue,
		StartsAt:   startsAt,
		TotalSeats: total,
		CreatedAt:  time.Now().UTC(),
	}
	s.events[evt.ID] = evt

	seatMap := make(map[string]*domain.Seat, total)
	for r := 0; r < rows; r++ {
		rowChar := rune('A' + r)
		for num := 1; num <= seatsPerRow; num++ {
			seatNo := fmt.Sprintf("%c-%d", rowChar, num)
			seatMap[seatNo] = &domain.Seat{
				ID:         fmt.Sprintf("%s-%s", evt.ID, seatNo),
				EventID:    evt.ID,
				SeatNumber: seatNo,
				PriceCents: priceCents,
				Status:     domain.SeatAvailable,
				Version:    1,
			}
		}
	}
	s.seatsByEvent[evt.ID] = seatMap
	return evt
}

func (s *BookingService) ListEvents() []domain.Event {
	s.mu.RLock()
	defer s.mu.RUnlock()
	res := make([]domain.Event, 0, len(s.events))
	for _, e := range s.events {
		res = append(res, e)
	}
	return res
}

func (s *BookingService) ListSeats(eventID string) ([]domain.Seat, error) {
	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()

	seatMap, ok := s.seatsByEvent[eventID]
	if !ok {
		return nil, domain.ErrSeatNotFound
	}

	res := make([]domain.Seat, 0, len(seatMap))
	for _, seat := range seatMap {
		if seat.Status == domain.SeatHeld && seat.HoldExpiresAt != nil && now.After(*seat.HoldExpiresAt) {
			s.releaseExpiredSeatLocked(seat)
		}
		res = append(res, *seat)
	}
	return res, nil
}

func (s *BookingService) HoldSeat(ctx context.Context, userID, eventID, seatNumber string) (domain.HoldReservation, error) {
	seatNumber = strings.ToUpper(strings.TrimSpace(seatNumber))
	lockKey := fmt.Sprintf("seat_lock:%s:%s", eventID, seatNumber)

	lockToken, acquired, err := s.locker.Acquire(ctx, lockKey, 3*time.Second)
	if err != nil {
		return domain.HoldReservation{}, err
	}
	if !acquired {
		return domain.HoldReservation{}, domain.ErrLockBusy
	}
	defer func() {
		_ = s.locker.Release(context.Background(), lockKey, lockToken)
	}()

	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()

	seatMap, ok := s.seatsByEvent[eventID]
	if !ok {
		return domain.HoldReservation{}, domain.ErrSeatNotFound
	}
	seat, ok := seatMap[seatNumber]
	if !ok {
		return domain.HoldReservation{}, domain.ErrSeatNotFound
	}

	if seat.Status == domain.SeatHeld && seat.HoldExpiresAt != nil && now.After(*seat.HoldExpiresAt) {
		s.releaseExpiredSeatLocked(seat)
	}

	if seat.Status != domain.SeatAvailable {
		return domain.HoldReservation{}, domain.ErrSeatAlreadyTaken
	}

	expiresAt := now.Add(s.holdTTL)
	holdID := fmt.Sprintf("hold-%d", s.seq.Add(1))

	seat.Version++
	seat.Status = domain.SeatHeld
	seat.HeldByUserID = userID
	seat.HoldID = holdID
	seat.HoldExpiresAt = &expiresAt

	hold := domain.HoldReservation{
		HoldID:      holdID,
		EventID:     eventID,
		SeatID:      seat.ID,
		SeatNumber:  seat.SeatNumber,
		UserID:      userID,
		PriceCents:  seat.PriceCents,
		ExpiresAt:   expiresAt,
		SeatVersion: seat.Version,
	}
	s.holds[holdID] = hold

	return hold, nil
}

func (s *BookingService) ConfirmBooking(ctx context.Context, userID, holdID, idempotencyKey string) (domain.Order, bool, error) {
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if idempotencyKey == "" {
		return domain.Order{}, false, domain.ErrMissingIdempotency
	}

	s.mu.RLock()
	if existingOrder, ok := s.ordersByIdemp[idempotencyKey]; ok {
		s.mu.RUnlock()
		if existingOrder.UserID != userID {
			return domain.Order{}, false, domain.ErrHoldOwnership
		}
		return existingOrder, true, nil
	}
	s.mu.RUnlock()

	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()

	if existingOrder, ok := s.ordersByIdemp[idempotencyKey]; ok {
		if existingOrder.UserID != userID {
			return domain.Order{}, false, domain.ErrHoldOwnership
		}
		return existingOrder, true, nil
	}

	hold, ok := s.holds[holdID]
	if !ok {
		return domain.Order{}, false, domain.ErrHoldNotFound
	}
	if hold.UserID != userID {
		return domain.Order{}, false, domain.ErrHoldOwnership
	}

	seatMap := s.seatsByEvent[hold.EventID]
	seat := seatMap[hold.SeatNumber]

	if now.After(hold.ExpiresAt) || seat.Status != domain.SeatHeld || seat.HoldID != holdID {
		if seat.Status == domain.SeatHeld && seat.HoldID == holdID {
			s.releaseExpiredSeatLocked(seat)
		}
		return domain.Order{}, false, domain.ErrHoldExpired
	}

	if seat.Version != hold.SeatVersion {
		return domain.Order{}, false, domain.ErrSeatAlreadyTaken
	}

	seat.Version++
	seat.Status = domain.SeatSold
	seat.HoldExpiresAt = nil

	order := domain.Order{
		ID:             fmt.Sprintf("ord-%d", s.seq.Add(1)),
		HoldID:         hold.HoldID,
		UserID:         userID,
		EventID:        hold.EventID,
		SeatID:         seat.ID,
		SeatNumber:     seat.SeatNumber,
		AmountCents:    hold.PriceCents,
		IdempotencyKey: idempotencyKey,
		Status:         "CONFIRMED",
		CreatedAt:      now,
	}

	delete(s.holds, holdID)
	s.ordersByIdemp[idempotencyKey] = order
	s.ordersByID[order.ID] = order

	return order, false, nil
}

func (s *BookingService) CleanupExpiredHolds() int {
	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()

	cleaned := 0
	for _, seatMap := range s.seatsByEvent {
		for _, seat := range seatMap {
			if seat.Status == domain.SeatHeld && seat.HoldExpiresAt != nil && now.After(*seat.HoldExpiresAt) {
				s.releaseExpiredSeatLocked(seat)
				cleaned++
			}
		}
	}
	if cleaned > 0 {
		s.expiredCleaned.Add(uint64(cleaned))
	}
	return cleaned
}

func (s *BookingService) releaseExpiredSeatLocked(seat *domain.Seat) {
	if seat.HoldID != "" {
		delete(s.holds, seat.HoldID)
	}
	seat.Version++
	seat.Status = domain.SeatAvailable
	seat.HeldByUserID = ""
	seat.HoldID = ""
	seat.HoldExpiresAt = nil
}

func (s *BookingService) StartHoldReaper(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.CleanupExpiredHolds()
			}
		}
	}()
}
