package service

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/user/ticket-booking/internal/auth"
	"github.com/user/ticket-booking/internal/domain"
	"github.com/user/ticket-booking/internal/lock"
)

func setupTestService(holdTTL time.Duration) (*BookingService, domain.Event) {
	locker := lock.NewRedisStyleLocker()
	jwtMgr := auth.NewManager("test-secret", 15*time.Minute, 24*time.Hour)
	svc := NewBookingService(locker, jwtMgr, holdTTL)
	evt := svc.CreateEvent("Go Highload Conf 2026", "Main Hall", time.Now().Add(48*time.Hour), 2, 5, 15000)
	return svc, evt
}

func TestConcurrentHold_ZeroOverbooking(t *testing.T) {
	svc, evt := setupTestService(10 * time.Minute)

	const contenders = 200
	var successCount atomic.Int32
	var conflictCount atomic.Int32
	var wg sync.WaitGroup
	wg.Add(contenders)

	startGate := make(chan struct{})

	for i := 0; i < contenders; i++ {
		go func(idx int) {
			defer wg.Done()
			<-startGate
			userID := fmt.Sprintf("user-%d", idx)
			_, err := svc.HoldSeat(context.Background(), userID, evt.ID, "A-1")
			if err == nil {
				successCount.Add(1)
			} else {
				conflictCount.Add(1)
			}
		}(i)
	}

	close(startGate)
	wg.Wait()

	if s := successCount.Load(); s != 1 {
		t.Fatalf("expected exactly 1 successful seat hold, got %d (overbooking detected!)", s)
	}
	if c := conflictCount.Load(); c != contenders-1 {
		t.Fatalf("expected %d rejected attempts, got %d", contenders-1, c)
	}
}

func TestHoldExpirationAndRebooking(t *testing.T) {
	svc, evt := setupTestService(40 * time.Millisecond)

	hold1, err := svc.HoldSeat(context.Background(), "user-1", evt.ID, "A-2")
	if err != nil {
		t.Fatalf("unexpected error on initial hold: %v", err)
	}

	time.Sleep(60 * time.Millisecond)
	svc.CleanupExpiredHolds()

	hold2, err := svc.HoldSeat(context.Background(), "user-2", evt.ID, "A-2")
	if err != nil {
		t.Fatalf("expected user-2 to hold seat after expiration, got error: %v", err)
	}

	_, _, err = svc.ConfirmBooking(context.Background(), "user-1", hold1.HoldID, "idemp-user-1")
	if err == nil {
		t.Fatalf("expected error when confirming expired hold1, got nil")
	}

	order, _, err := svc.ConfirmBooking(context.Background(), "user-2", hold2.HoldID, "idemp-user-2")
	if err != nil {
		t.Fatalf("unexpected error confirming hold2: %v", err)
	}
	if order.Status != "CONFIRMED" {
		t.Fatalf("expected CONFIRMED status, got %s", order.Status)
	}
}

func TestIdempotentPaymentConfirmation(t *testing.T) {
	svc, evt := setupTestService(10 * time.Minute)

	hold, err := svc.HoldSeat(context.Background(), "user-77", evt.ID, "B-3")
	if err != nil {
		t.Fatalf("unexpected hold error: %v", err)
	}

	ord1, replayed1, err := svc.ConfirmBooking(context.Background(), "user-77", hold.HoldID, "pay-key-999")
	if err != nil {
		t.Fatalf("first confirmation failed: %v", err)
	}
	if replayed1 {
		t.Fatalf("expected replayed=false on first confirmation")
	}

	ord2, replayed2, err := svc.ConfirmBooking(context.Background(), "user-77", hold.HoldID, "pay-key-999")
	if err != nil {
		t.Fatalf("second idempotent confirmation failed: %v", err)
	}
	if !replayed2 {
		t.Fatalf("expected replayed=true on duplicate request")
	}
	if ord1.ID != ord2.ID {
		t.Fatalf("expected identical order ID %s, got %s", ord1.ID, ord2.ID)
	}
}

func BenchmarkHoldConflictContention(b *testing.B) {
	svc, evt := setupTestService(10 * time.Minute)
	_, _ = svc.HoldSeat(context.Background(), "initial-holder", evt.ID, "A-1")

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = svc.HoldSeat(context.Background(), "contender", evt.ID, "A-1")
		}
	})
}
