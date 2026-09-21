package patient_queue

import (
	"context"
	"errors"
	"testing"
	"time"
)

// fakeWorkerLoopObserver records Observe* calls for LOT 26I-5B/5C unit tests.
type fakeWorkerLoopObserver struct {
	ticks          []tickObs
	claimed        []int
	staleRecovered []int
	deliveries     []deliveryObs
	providers      []providerObs
}

type tickObs struct {
	duration time.Duration
	err      error
}

func (f *fakeWorkerLoopObserver) ObserveTick(d time.Duration, err error) {
	f.ticks = append(f.ticks, tickObs{duration: d, err: err})
}
func (f *fakeWorkerLoopObserver) ObserveClaimed(n int) {
	f.claimed = append(f.claimed, n)
}
func (f *fakeWorkerLoopObserver) ObserveStaleRecovered(n int) {
	f.staleRecovered = append(f.staleRecovered, n)
}

// scriptedLease drives Tick recover/claim without Postgres.
type scriptedLease struct {
	recoverN   int
	recoverErr error
	claimed    []AppointmentNotificationIntent
	claimErr   error
}

func (s *scriptedLease) RecoverStaleProcessingClaims(time.Time, int) (int, error) {
	return s.recoverN, s.recoverErr
}
func (s *scriptedLease) ClaimDueNotificationIntents(time.Time, int, []string) ([]AppointmentNotificationIntent, error) {
	return s.claimed, s.claimErr
}

func testWorkerWithLease(t *testing.T, lease notificationLeaseStore, obs WorkerLoopObserver) *NotificationWorker {
	t.Helper()
	ad := NewNoopDeliveryAdapter(NotifChannelLog)
	w, err := NewNotificationWorker(nil, NotificationWorkerConfig{
		Adapters: map[string]NotificationDeliveryAdapter{NotifChannelLog: ad},
		Observer: obs,
	})
	if err != nil {
		t.Fatal(err)
	}
	w.lease = lease
	return w
}

func TestNotificationWorkerNilObserverNoop(t *testing.T) {
	t.Parallel()
	w, err := NewNotificationWorker(nil, NotificationWorkerConfig{
		Adapters: map[string]NotificationDeliveryAdapter{
			NotifChannelLog: NewNoopDeliveryAdapter(NotifChannelLog),
		},
		Observer: nil,
	})
	if err != nil {
		t.Fatal(err)
	}
	if w.observer == nil {
		t.Fatal("observer must be normalized to non-nil no-op")
	}
	w.lease = &scriptedLease{}
	if err := w.Tick(context.Background()); err != nil {
		t.Fatalf("Tick with nil observer: %v", err)
	}
}

func TestNotificationWorkerObserverSuccessEmptyClaims(t *testing.T) {
	t.Parallel()
	obs := &fakeWorkerLoopObserver{}
	w := testWorkerWithLease(t, &scriptedLease{}, obs)
	if err := w.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(obs.ticks) != 1 || obs.ticks[0].err != nil {
		t.Fatalf("ticks=%v want one success", obs.ticks)
	}
	if obs.ticks[0].duration < 0 {
		t.Fatal("duration must be non-negative")
	}
	if len(obs.claimed) != 1 || obs.claimed[0] != 0 {
		t.Fatalf("claimed=%v", obs.claimed)
	}
	if len(obs.staleRecovered) != 1 || obs.staleRecovered[0] != 0 {
		t.Fatalf("stale=%v", obs.staleRecovered)
	}
}

func TestNotificationWorkerObserverTickErrorOnRecover(t *testing.T) {
	t.Parallel()
	obs := &fakeWorkerLoopObserver{}
	boom := errors.New("recover boom")
	w := testWorkerWithLease(t, &scriptedLease{recoverErr: boom}, obs)
	err := w.Tick(context.Background())
	if !errors.Is(err, boom) {
		t.Fatalf("err=%v", err)
	}
	if len(obs.ticks) != 1 || obs.ticks[0].err == nil {
		t.Fatalf("want one error tick, got %#v", obs.ticks)
	}
	if len(obs.claimed) != 0 {
		t.Fatalf("claim must not be observed on recover failure: %v", obs.claimed)
	}
	if len(obs.staleRecovered) != 1 || obs.staleRecovered[0] != 0 {
		t.Fatalf("stale=%v", obs.staleRecovered)
	}
}

func TestNotificationWorkerObserverClaimError(t *testing.T) {
	t.Parallel()
	obs := &fakeWorkerLoopObserver{}
	boom := errors.New("claim boom")
	w := testWorkerWithLease(t, &scriptedLease{claimErr: boom}, obs)
	err := w.Tick(context.Background())
	if !errors.Is(err, boom) {
		t.Fatalf("err=%v", err)
	}
	if len(obs.ticks) != 1 || obs.ticks[0].err == nil {
		t.Fatalf("ticks=%v", obs.ticks)
	}
	if len(obs.claimed) != 0 {
		t.Fatalf("claimed must not increment on claim error: %v", obs.claimed)
	}
}

func TestNotificationWorkerObserverClaimedN(t *testing.T) {
	t.Parallel()
	obs := &fakeWorkerLoopObserver{}
	claimed := []AppointmentNotificationIntent{
		{ID: 1, Channel: NotifChannelLog, Status: NotifStatusProcessing},
		{ID: 2, Channel: NotifChannelLog, Status: NotifStatusProcessing},
		{ID: 3, Channel: NotifChannelLog, Status: NotifStatusProcessing},
	}
	// processClaimed needs *Service; leave svc nil and cancel ctx before loop so
	// claim count is observed without delivery (ctx cancel after claim).
	w := testWorkerWithLease(t, &scriptedLease{claimed: claimed}, obs)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := w.Tick(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v want canceled", err)
	}
	if len(obs.claimed) != 1 || obs.claimed[0] != 3 {
		t.Fatalf("claimed=%v want 3", obs.claimed)
	}
	if len(obs.ticks) != 1 || obs.ticks[0].err == nil {
		t.Fatalf("ctx cancel must be Tick error: %#v", obs.ticks)
	}
}

func TestNotificationWorkerObserverStaleRecoveredN(t *testing.T) {
	t.Parallel()
	obs := &fakeWorkerLoopObserver{}
	w := testWorkerWithLease(t, &scriptedLease{recoverN: 7}, obs)
	if err := w.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(obs.staleRecovered) != 1 || obs.staleRecovered[0] != 7 {
		t.Fatalf("stale=%v", obs.staleRecovered)
	}
	if len(obs.ticks) != 1 || obs.ticks[0].err != nil {
		t.Fatalf("ticks=%v", obs.ticks)
	}
}

func TestNotificationWorkerObserverPartialStaleRecoveredWithError(t *testing.T) {
	t.Parallel()
	obs := &fakeWorkerLoopObserver{}
	boom := errors.New("partial recover")
	w := testWorkerWithLease(t, &scriptedLease{recoverN: 4, recoverErr: boom}, obs)
	err := w.Tick(context.Background())
	if !errors.Is(err, boom) {
		t.Fatalf("err=%v", err)
	}
	if len(obs.staleRecovered) != 1 || obs.staleRecovered[0] != 4 {
		t.Fatalf("partial n must be observed before error return: %v", obs.staleRecovered)
	}
	if len(obs.ticks) != 1 || obs.ticks[0].err == nil {
		t.Fatalf("ticks=%v", obs.ticks)
	}
	if len(obs.claimed) != 0 {
		t.Fatal("claim must not run after recover error")
	}
}

func TestNotificationWorkerObserverZeroStaleNoPositive(t *testing.T) {
	t.Parallel()
	obs := &fakeWorkerLoopObserver{}
	w := testWorkerWithLease(t, &scriptedLease{recoverN: 0}, obs)
	_ = w.Tick(context.Background())
	if len(obs.staleRecovered) != 1 || obs.staleRecovered[0] != 0 {
		t.Fatalf("stale=%v", obs.staleRecovered)
	}
}

func TestNotificationWorkerObserverMultipleTicksNoDuplicate(t *testing.T) {
	t.Parallel()
	obs := &fakeWorkerLoopObserver{}
	w := testWorkerWithLease(t, &scriptedLease{recoverN: 1, claimed: nil}, obs)
	for i := 0; i < 3; i++ {
		if err := w.Tick(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if len(obs.ticks) != 3 {
		t.Fatalf("ticks=%d want 3", len(obs.ticks))
	}
	if len(obs.staleRecovered) != 3 {
		t.Fatalf("stale calls=%d", len(obs.staleRecovered))
	}
	for _, n := range obs.staleRecovered {
		if n != 1 {
			t.Fatalf("stale=%v", obs.staleRecovered)
		}
	}
}
