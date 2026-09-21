package patient_queue

import (
	"context"
	"errors"
	"testing"
	"time"
)

// fakeQueueSnapshotter records NotificationQueueSnapshot calls for Tick tests.
type fakeQueueSnapshotter struct {
	calls int
	asOfs []time.Time
	snap  NotificationQueueSnapshot
	err   error
}

func (f *fakeQueueSnapshotter) NotificationQueueSnapshot(_ context.Context, asOf time.Time) (NotificationQueueSnapshot, error) {
	f.calls++
	f.asOfs = append(f.asOfs, asOf)
	if f.err != nil {
		return NotificationQueueSnapshot{}, f.err
	}
	return f.snap, nil
}

type fakeQueueObserver struct {
	snaps []NotificationQueueSnapshot
}

func (f *fakeQueueObserver) ObserveQueueSnapshot(s NotificationQueueSnapshot) {
	f.snaps = append(f.snaps, s)
}

func testWorkerWithLeaseAndQueue(
	t *testing.T,
	lease notificationLeaseStore,
	queue notificationQueueSnapshotter,
	obs WorkerLoopObserver,
	qObs NotificationQueueSnapshotObserver,
) *NotificationWorker {
	t.Helper()
	w, err := NewNotificationWorker(nil, NotificationWorkerConfig{
		Adapters: map[string]NotificationDeliveryAdapter{
			NotifChannelLog: NewNoopDeliveryAdapter(NotifChannelLog),
		},
		Observer:      obs,
		QueueObserver: qObs,
	})
	if err != nil {
		t.Fatal(err)
	}
	w.lease = lease
	w.queue = queue
	return w
}

func TestNotificationWorkerQueueSnapshotSuccessOncePerTick(t *testing.T) {
	t.Parallel()
	obs := &fakeWorkerLoopObserver{}
	qObs := &fakeQueueObserver{}
	snap := NotificationQueueSnapshot{
		AsOf: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Channels: []NotificationQueueChannelSnapshot{
			{Channel: NotifChannelLog, Pending: 2, Due: 1},
		},
	}
	q := &fakeQueueSnapshotter{snap: snap}
	w := testWorkerWithLeaseAndQueue(t, &scriptedLease{}, q, obs, qObs)

	if err := w.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if q.calls != 1 {
		t.Fatalf("snapshot calls=%d want 1", q.calls)
	}
	if len(qObs.snaps) != 1 {
		t.Fatalf("observer snaps=%d want 1", len(qObs.snaps))
	}
	if qObs.snaps[0].Channels[0].Pending != 2 {
		t.Fatalf("snap=%+v", qObs.snaps[0])
	}
	if len(obs.ticks) != 1 || obs.ticks[0].err != nil {
		t.Fatalf("Tick must remain success: %#v", obs.ticks)
	}
}

func TestNotificationWorkerQueueSnapshotFailureBestEffort(t *testing.T) {
	t.Parallel()
	obs := &fakeWorkerLoopObserver{}
	qObs := &fakeQueueObserver{}
	q := &fakeQueueSnapshotter{err: errors.New("snapshot boom")}
	w := testWorkerWithLeaseAndQueue(t, &scriptedLease{recoverN: 2}, q, obs, qObs)

	if err := w.Tick(context.Background()); err != nil {
		t.Fatalf("snapshot error must not fail Tick: %v", err)
	}
	if q.calls != 1 {
		t.Fatalf("calls=%d", q.calls)
	}
	if len(qObs.snaps) != 0 {
		t.Fatalf("observer must not be called on snapshot failure: %#v", qObs.snaps)
	}
	if len(obs.ticks) != 1 || obs.ticks[0].err != nil {
		t.Fatalf("ticks=%#v", obs.ticks)
	}
	if len(obs.staleRecovered) != 1 || obs.staleRecovered[0] != 2 {
		t.Fatalf("primary work must still run: stale=%v", obs.staleRecovered)
	}
}

func TestNotificationWorkerQueueSnapshotSkippedWhenCtxCanceled(t *testing.T) {
	t.Parallel()
	// Existing cancellation: claim observed, then return ctx.Err() before process loop
	// → snapshot refresh must not run (preserve cancellation semantics).
	obs := &fakeWorkerLoopObserver{}
	qObs := &fakeQueueObserver{}
	q := &fakeQueueSnapshotter{snap: NotificationQueueSnapshot{}}
	claimed := []AppointmentNotificationIntent{
		{ID: 1, Channel: NotifChannelLog, Status: NotifStatusProcessing},
	}
	w := testWorkerWithLeaseAndQueue(t, &scriptedLease{claimed: claimed}, q, obs, qObs)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := w.Tick(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
	if q.calls != 0 {
		t.Fatalf("snapshot must not run after cancel: calls=%d", q.calls)
	}
	if len(qObs.snaps) != 0 {
		t.Fatal("observer must not be called")
	}
}

func TestNotificationWorkerQueueSnapshotNotOnRecoverError(t *testing.T) {
	t.Parallel()
	obs := &fakeWorkerLoopObserver{}
	qObs := &fakeQueueObserver{}
	q := &fakeQueueSnapshotter{}
	boom := errors.New("recover boom")
	w := testWorkerWithLeaseAndQueue(t, &scriptedLease{recoverErr: boom}, q, obs, qObs)
	if err := w.Tick(context.Background()); !errors.Is(err, boom) {
		t.Fatalf("err=%v", err)
	}
	if q.calls != 0 {
		t.Fatalf("snapshot must not run after recover error: %d", q.calls)
	}
}

func TestNotificationWorkerNilQueueObserverNoop(t *testing.T) {
	t.Parallel()
	w, err := NewNotificationWorker(nil, NotificationWorkerConfig{
		Adapters: map[string]NotificationDeliveryAdapter{
			NotifChannelLog: NewNoopDeliveryAdapter(NotifChannelLog),
		},
		QueueObserver: nil,
	})
	if err != nil {
		t.Fatal(err)
	}
	if w.queueObserver == nil {
		t.Fatal("QueueObserver must normalize to no-op")
	}
	w.lease = &scriptedLease{}
	// queue remains nil → refresh no-op
	if err := w.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestNotificationWorkerQueueSnapshotMultipleTicksOnceEach(t *testing.T) {
	t.Parallel()
	qObs := &fakeQueueObserver{}
	q := &fakeQueueSnapshotter{snap: NotificationQueueSnapshot{}}
	w := testWorkerWithLeaseAndQueue(t, &scriptedLease{}, q, &fakeWorkerLoopObserver{}, qObs)
	for i := 0; i < 3; i++ {
		if err := w.Tick(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if q.calls != 3 || len(qObs.snaps) != 3 {
		t.Fatalf("calls=%d snaps=%d want 3 each", q.calls, len(qObs.snaps))
	}
}
