package patient_queue

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/lallene/medcore-his/backend/internal/shared/email"
	"gorm.io/gorm"
)

func TestClassifyWorkerLogError(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"nil", nil, WorkerErrorClassInternal},
		{"canceled", context.Canceled, WorkerErrorClassCanceled},
		{"deadline", context.DeadlineExceeded, WorkerErrorClassCanceled},
		{"ambiguous", email.ErrAmbiguousDelivery, WorkerErrorClassAmbiguous},
		{"ambiguous wrapped", email.AmbiguousDelivery(errors.New("x")), WorkerErrorClassAmbiguous},
		{"ambiguous wraps canceled", email.AmbiguousDelivery(context.Canceled), WorkerErrorClassAmbiguous},
		{"ambiguous wraps deadline", email.AmbiguousDelivery(context.DeadlineExceeded), WorkerErrorClassAmbiguous},
		{"permanent", email.ErrPermanent, WorkerErrorClassPermanent},
		{"invalid_message", email.ErrInvalidMessage, WorkerErrorClassInvalidMessage},
		{"not_configured", email.ErrNotConfigured, WorkerErrorClassNotConfigured},
		{"transient", email.ErrTransient, WorkerErrorClassTransient},
		{"transient wrapped", email.Transient(errors.New("x")), WorkerErrorClassTransient},
		{"gorm not found", gorm.ErrRecordNotFound, WorkerErrorClassDatabase},
		{"gorm invalid db", gorm.ErrInvalidDB, WorkerErrorClassDatabase},
		{"unknown", errors.New("RAW_SQL_SECRET_MARKER SELECT * FROM patients"), WorkerErrorClassInternal},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := ClassifyWorkerLogError(tc.err)
			if got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
			// Classification output must never echo raw error text.
			if tc.err != nil && strings.Contains(got, "RAW_SQL_SECRET_MARKER") {
				t.Fatal("error_class leaked raw error text")
			}
			if tc.err != nil && strings.Contains(got, tc.err.Error()) && tc.err.Error() != "" {
				// Classes are short tokens; only fail if full error string leaked.
				if len(got) > 32 {
					t.Fatalf("error_class looks like raw error: %q", got)
				}
			}
		})
	}
}

type recordingHandler struct {
	records []slog.Record
	attrs   [][]slog.Attr
}

func (h *recordingHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *recordingHandler) Handle(_ context.Context, r slog.Record) error {
	h.records = append(h.records, r.Clone())
	var as []slog.Attr
	r.Attrs(func(a slog.Attr) bool {
		as = append(as, a)
		return true
	})
	h.attrs = append(h.attrs, as)
	return nil
}
func (h *recordingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *recordingHandler) WithGroup(string) slog.Handler      { return h }
func (h *recordingHandler) text() string {
	var b strings.Builder
	for i, r := range h.records {
		b.WriteString(r.Message)
		b.WriteByte(' ')
		for _, a := range h.attrs[i] {
			b.WriteString(a.Key)
			b.WriteByte('=')
			b.WriteString(a.Value.String())
			b.WriteByte(' ')
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func assertLogLacksMarkers(t *testing.T, out string, markers ...string) {
	t.Helper()
	for _, m := range markers {
		if strings.Contains(out, m) {
			t.Fatalf("log leaked marker %q in %q", m, out)
		}
	}
}

func TestLogDeliveryAdapterNoIdentifyingApplicationLog(t *testing.T) {
	t.Parallel()
	const (
		intentMarker      = "INTENTID_MARKER_991122"
		apptMarker        = "APPOINTMENTID_MARKER_334455"
		emailMarker       = "patient@LEAK-MARKER.invalid"
		subjectMarker     = "SUBJECT_SECRET_MARKER"
		bodyMarker        = "BODY_SECRET_MARKER"
		payloadMarker     = "PAYLOAD_SECRET_MARKER"
		secretMarker      = "CLIENT_SECRET_MARKER"
		dsnMarker         = "postgres://DSN_SECRET_MARKER"
		idempotencyMarker = "idempotency-SECRET-MARKER"
		schedMarker       = "2099-01-02T03:04:05Z_SCHED_MARKER"
	)
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	ad := NewLogDeliveryAdapter(NotifChannelLog, logger)
	intent := &AppointmentNotificationIntent{
		ID:            991122,
		AppointmentID: 334455,
		PatientID:     778899,
		Kind:          NotifKindBooked,
		Channel:       NotifChannelLog,
		PayloadJSON:   `{"appointmentId":334455,"scheduledAt":"2099-01-02T03:04:05Z","` + payloadMarker + `":"x"}`,
	}
	payload := NotificationPayload{
		AppointmentID: 334455,
		ScheduledAt:   schedMarker,
		ClinicLabel:   subjectMarker + bodyMarker + emailMarker + secretMarker + dsnMarker + idempotencyMarker,
	}
	res, err := ad.Send(context.Background(), intent, payload)
	if err != nil {
		t.Fatal(err)
	}
	if res.ProviderMessageID != "log" || res.Skipped {
		t.Fatalf("res=%+v", res)
	}
	if ad.ProviderName() != "log" {
		t.Fatal(ad.ProviderName())
	}
	out := buf.String()
	assertLogLacksMarkers(t, out,
		intentMarker, "991122", apptMarker, "334455",
		emailMarker, subjectMarker, bodyMarker, payloadMarker,
		secretMarker, dsnMarker, idempotencyMarker, schedMarker,
		"intentId", "appointmentId", "scheduledAt", "patientId",
	)
}

type boomFinalizer struct {
	err error
}

func (f *boomFinalizer) failOrRetryAfterAttempt(uint, string, *string, *string, time.Time, time.Duration) error {
	return f.err
}
func (f *boomFinalizer) FinalizeNotificationSent(uint, string, *string) (*AppointmentNotificationAttempt, error) {
	return nil, f.err
}
func (f *boomFinalizer) FinalizeNotificationSkipped(uint, string, *string) (*AppointmentNotificationAttempt, error) {
	return nil, f.err
}
func (f *boomFinalizer) FinalizeNotificationFailedTerminal(uint, string, *string, *string) (*AppointmentNotificationAttempt, error) {
	return nil, f.err
}

func TestWorkerApplicationLogsPrivacy(t *testing.T) {
	t.Parallel()
	const (
		rawSQL   = "RAW_SQL_SECRET_MARKER SELECT * FROM appointment_notification_intents"
		dsnMark  = "DSN_SECRET_MARKER_postgres://x"
		provBody = "PROVIDER_BODY_SECRET_MARKER"
		intentID = uint(424242)
		apptID   = uint(535353)
	)
	sensitiveErr := errors.New(rawSQL + " " + dsnMark + " " + provBody)

	h := &recordingHandler{}
	log := slog.New(h)

	t.Run("tick_failure", func(t *testing.T) {
		h.records, h.attrs = nil, nil
		w, err := NewNotificationWorker(nil, NotificationWorkerConfig{
			Adapters: map[string]NotificationDeliveryAdapter{NotifChannelLog: NewNoopDeliveryAdapter(NotifChannelLog)},
			Logger:   log,
			// Tiny poll so Run can exit quickly after first Tick on canceled ctx.
			PollInterval: time.Millisecond,
		})
		if err != nil {
			t.Fatal(err)
		}
		w.lease = &scriptedLease{recoverErr: sensitiveErr}
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Tick still runs once; then Run returns without sleeping on ticker.
		_ = w.Run(ctx)
		out := h.text()
		assertLogLacksMarkers(t, out, rawSQL, dsnMark, provBody, "424242", "535353")
		if !strings.Contains(out, WorkerLogOpTick) || !strings.Contains(out, WorkerErrorClassInternal) {
			t.Fatalf("want operation/error_class: %q", out)
		}
	})

	t.Run("snapshot_failure", func(t *testing.T) {
		h.records, h.attrs = nil, nil
		w, err := NewNotificationWorker(nil, NotificationWorkerConfig{
			Adapters: map[string]NotificationDeliveryAdapter{NotifChannelLog: NewNoopDeliveryAdapter(NotifChannelLog)},
			Logger:   log,
		})
		if err != nil {
			t.Fatal(err)
		}
		w.lease = &scriptedLease{}
		w.queue = &fakeQueueSnapshotter{err: sensitiveErr}
		if err := w.Tick(context.Background()); err != nil {
			t.Fatalf("snapshot must not fail Tick: %v", err)
		}
		out := h.text()
		assertLogLacksMarkers(t, out, rawSQL, dsnMark, provBody)
		if !strings.Contains(out, WorkerLogOpQueueSnapshotRefresh) || !strings.Contains(out, WorkerErrorClassInternal) {
			t.Fatalf("want snapshot op: %q", out)
		}
	})

	t.Run("finalize_sent_failure", func(t *testing.T) {
		h.records, h.attrs = nil, nil
		ad := &scriptedAdapter{channel: NotifChannelLog, provider: "log", result: DeliveryResult{ProviderMessageID: "log"}}
		w, err := NewNotificationWorker(nil, NotificationWorkerConfig{
			Adapters: map[string]NotificationDeliveryAdapter{NotifChannelLog: ad},
			Logger:   log,
		})
		if err != nil {
			t.Fatal(err)
		}
		w.finalizer = &boomFinalizer{err: sensitiveErr}
		intent := &AppointmentNotificationIntent{
			ID: intentID, AppointmentID: apptID, Channel: NotifChannelLog, Kind: NotifKindBooked,
			PayloadJSON: `{"appointmentId":535353,"scheduledAt":"2026-01-01T10:00:00Z"}`,
		}
		w.processClaimed(context.Background(), intent, time.Now().UTC())
		out := h.text()
		assertLogLacksMarkers(t, out, rawSQL, dsnMark, provBody, "424242", "535353", "intentId", "appointmentId")
		if !strings.Contains(out, WorkerLogOpFinalizeSent) || !strings.Contains(out, WorkerErrorClassInternal) {
			t.Fatalf("want finalize_sent: %q", out)
		}
	})

	t.Run("pre_send_skip_no_info", func(t *testing.T) {
		h.records, h.attrs = nil, nil
		ad := NewNoopDeliveryAdapter(NotifChannelLog)
		w, err := NewNotificationWorker(nil, NotificationWorkerConfig{
			Adapters: map[string]NotificationDeliveryAdapter{NotifChannelLog: ad},
			Logger:   log,
		})
		if err != nil {
			t.Fatal(err)
		}
		w.finalizer = &okFinalizer{}
		w.preSendCheck = func(*AppointmentNotificationIntent, NotificationPayload) (bool, string) {
			return true, "appointment cancelled"
		}
		intent := &AppointmentNotificationIntent{
			ID: intentID, AppointmentID: apptID, Channel: NotifChannelLog, Kind: NotifKindReminderT24H,
			PayloadJSON: `{"appointmentId":535353,"scheduledAt":"2026-01-01T10:00:00Z"}`,
		}
		w.processClaimed(context.Background(), intent, time.Now().UTC())
		out := h.text()
		if strings.Contains(out, "appointment_notification_skip") {
			t.Fatal("routine skip Info must be removed")
		}
		assertLogLacksMarkers(t, out, "424242", "535353", "intentId", "appointmentId")
	})

	t.Run("log_adapter_success_quiet", func(t *testing.T) {
		h.records, h.attrs = nil, nil
		ad := NewLogDeliveryAdapter(NotifChannelLog, log)
		w, err := NewNotificationWorker(nil, NotificationWorkerConfig{
			Adapters: map[string]NotificationDeliveryAdapter{NotifChannelLog: ad},
			Logger:   log,
		})
		if err != nil {
			t.Fatal(err)
		}
		w.finalizer = &okFinalizer{}
		intent := &AppointmentNotificationIntent{
			ID: intentID, AppointmentID: apptID, Channel: NotifChannelLog, Kind: NotifKindBooked,
			PayloadJSON: `{"appointmentId":535353,"scheduledAt":"2026-01-01T10:00:00Z"}`,
		}
		w.processClaimed(context.Background(), intent, time.Now().UTC())
		out := h.text()
		assertLogLacksMarkers(t, out, "424242", "535353", "intentId", "appointmentId", "scheduledAt",
			"appointment_notification_intent")
	})
}

type okFinalizer struct{}

func (okFinalizer) failOrRetryAfterAttempt(uint, string, *string, *string, time.Time, time.Duration) error {
	return nil
}
func (okFinalizer) FinalizeNotificationSent(uint, string, *string) (*AppointmentNotificationAttempt, error) {
	return &AppointmentNotificationAttempt{}, nil
}
func (okFinalizer) FinalizeNotificationSkipped(uint, string, *string) (*AppointmentNotificationAttempt, error) {
	return &AppointmentNotificationAttempt{}, nil
}
func (okFinalizer) FinalizeNotificationFailedTerminal(uint, string, *string, *string) (*AppointmentNotificationAttempt, error) {
	return &AppointmentNotificationAttempt{}, nil
}
