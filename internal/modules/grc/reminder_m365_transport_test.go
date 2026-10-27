package grc

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lallene/medcore-his/backend/internal/shared/email/microsoft365"
)

func TestGRCReminderUsesGraphTransportOnce(t *testing.T) {
	f := newReminderFixture(t)
	now := evidenceTestNow
	soon := now.Add(2 * time.Hour)
	evidence := f.reminderEvidence(t, "EV-GRAPH", EvidenceStatusValid, &f.profile.ID, &soon)
	if n := detectReminders(t, f, now); n != 1 {
		t.Fatalf("created %d", n)
	}
	rows := reminderRows(t, f)
	claimReminderForTest(t, f, rows[0].ID, now, 1)

	var tokenCalls, sendCalls atomic.Int32
	const secret = "local-test-secret"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/token":
			tokenCalls.Add(1)
			body, _ := io.ReadAll(r.Body)
			if strings.Contains(string(body), secret) {
				w.Header().Set("Content-Type", "application/json")
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"access_token":"local-test-token","expires_in":3600,"token_type":"Bearer"}`)
		case strings.HasSuffix(r.URL.Path, "/sendMail"):
			sendCalls.Add(1)
			if r.Method != http.MethodPost {
				t.Errorf("method %s", r.Method)
			}
			if !strings.Contains(r.URL.Path, "noreply@example.com") {
				t.Errorf("sender path %s", r.URL.Path)
			}
			if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
				t.Error("authorization missing")
			}
			if r.Header.Get("Content-Type") != "application/json" {
				t.Errorf("content type %s", r.Header.Get("Content-Type"))
			}
			body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
			text := string(body)
			if strings.Contains(text, secret) || strings.Contains(text, "local-test-token") {
				t.Fatal("graph body contains a credential")
			}
			if !strings.Contains(text, evidence.Reference) || !strings.Contains(text, reminderSubjectEvidence) {
				t.Fatalf("graph body missing reminder fields")
			}
			w.WriteHeader(http.StatusAccepted)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	transport := newTestGraphTransport(t, srv, time.Second)
	worker, err := NewReminderWorker(f.svc, transport, DefaultReminderWindows(), ReminderLeaseDefault, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if err = worker.deliverOne(context.Background(), now, rows[0]); err != nil {
		t.Fatal(err)
	}
	if tokenCalls.Load() != 1 || sendCalls.Load() != 1 {
		t.Fatalf("token=%d send=%d", tokenCalls.Load(), sendCalls.Load())
	}
	var stored GRCReminderIntent
	if err = f.db.First(&stored, rows[0].ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Status != ReminderStatusDelivered {
		t.Fatalf("status %s", stored.Status)
	}
}

func TestGRCReminderGraphFailuresStayClassified(t *testing.T) {
	f := newReminderFixture(t)
	now := evidenceTestNow
	soon := now.Add(2 * time.Hour)
	f.reminderEvidence(t, "EV-GRAPH-FAIL", EvidenceStatusValid, &f.profile.ID, &soon)
	if n := detectReminders(t, f, now); n != 1 {
		t.Fatalf("created %d", n)
	}
	row := reminderRows(t, f)[0]

	t.Run("token 401", func(t *testing.T) {
		claimReminderForTest(t, f, row.ID, now, 1)
		var sends atomic.Int32
		srv := graphStatusServer(t, http.StatusUnauthorized, http.StatusAccepted, &sends)
		defer srv.Close()
		worker := graphWorker(t, f, srv, time.Second)
		if err := worker.deliverOne(context.Background(), now, row); err == nil || !strings.Contains(err.Error(), reminderErrPermanent) {
			t.Fatalf("err %v", err)
		}
		if sends.Load() != 0 {
			t.Fatalf("sendMail calls %d", sends.Load())
		}
		assertReminder(t, f, row.ID, ReminderStatusFailed, ReminderMaxAttempts, reminderErrPermanent)
	})
}

func TestGRCReminderGraphSendFailureIsRetryable(t *testing.T) {
	f := newReminderFixture(t)
	now := evidenceTestNow
	soon := now.Add(2 * time.Hour)
	f.reminderEvidence(t, "EV-GRAPH-500", EvidenceStatusValid, &f.profile.ID, &soon)
	row := mustOneReminder(t, f, now)
	claimReminderForTest(t, f, row.ID, now, 1)
	var sends atomic.Int32
	srv := graphStatusServer(t, http.StatusOK, http.StatusInternalServerError, &sends)
	defer srv.Close()
	worker := graphWorker(t, f, srv, time.Second)
	if err := worker.deliverOne(context.Background(), now, row); err == nil || !strings.Contains(err.Error(), reminderErrTransient) {
		t.Fatalf("err %v", err)
	}
	if sends.Load() != 1 {
		t.Fatalf("sendMail calls %d", sends.Load())
	}
	assertReminder(t, f, row.ID, ReminderStatusFailed, 1, reminderErrTransient)
}

func TestGRCReminderGraph429IsOneRetryableFailure(t *testing.T) {
	f := newReminderFixture(t)
	now := evidenceTestNow
	soon := now.Add(2 * time.Hour)
	f.reminderEvidence(t, "EV-GRAPH-429", EvidenceStatusValid, &f.profile.ID, &soon)
	row := mustOneReminder(t, f, now)
	claimReminderForTest(t, f, row.ID, now, 1)
	var sends atomic.Int32
	srv := graphStatusServer(t, http.StatusOK, http.StatusTooManyRequests, &sends)
	defer srv.Close()
	worker := graphWorker(t, f, srv, time.Second)
	if err := worker.deliverOne(context.Background(), now, row); err == nil || !strings.Contains(err.Error(), reminderErrTransient) {
		t.Fatalf("err %v", err)
	}
	if sends.Load() != 1 {
		t.Fatalf("sendMail calls %d", sends.Load())
	}
	assertReminder(t, f, row.ID, ReminderStatusFailed, 1, reminderErrTransient)
}

func TestGRCReminderStaleTargetDoesNotCallGraph(t *testing.T) {
	f := newReminderFixture(t)
	now := evidenceTestNow
	soon := now.Add(2 * time.Hour)
	evidence := f.reminderEvidence(t, "EV-GRAPH-STALE", EvidenceStatusValid, &f.profile.ID, &soon)
	row := mustOneReminder(t, f, now)
	if err := f.db.Model(&Evidence{}).Where("id = ?", evidence.ID).Update("status", EvidenceStatusRevoked).Error; err != nil {
		t.Fatal(err)
	}
	claimReminderForTest(t, f, row.ID, now, 1)
	var sends atomic.Int32
	srv := graphStatusServer(t, http.StatusOK, http.StatusAccepted, &sends)
	defer srv.Close()
	worker := graphWorker(t, f, srv, time.Second)
	if err := worker.deliverOne(context.Background(), now, row); err == nil || !strings.Contains(err.Error(), reminderErrObsolete) {
		t.Fatalf("err %v", err)
	}
	if sends.Load() != 0 {
		t.Fatalf("sendMail calls %d", sends.Load())
	}
}

func TestGRCReminderGraphTimeoutEnds(t *testing.T) {
	f := newReminderFixture(t)
	now := evidenceTestNow
	soon := now.Add(2 * time.Hour)
	f.reminderEvidence(t, "EV-GRAPH-TIMEOUT", EvidenceStatusValid, &f.profile.ID, &soon)
	row := mustOneReminder(t, f, now)
	claimReminderForTest(t, f, row.ID, now, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	worker := graphWorker(t, f, srv, 50*time.Millisecond)
	start := time.Now()
	err := worker.deliverOne(context.Background(), now, row)
	if err == nil || !strings.Contains(err.Error(), reminderErrTransient) {
		t.Fatalf("err %v", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("timeout took %s", time.Since(start))
	}
	assertReminder(t, f, row.ID, ReminderStatusFailed, 1, reminderErrTransient)
}

func mustOneReminder(t *testing.T, f grc01DServiceFixture, now time.Time) GRCReminderIntent {
	t.Helper()
	if n := detectReminders(t, f, now); n != 1 {
		t.Fatalf("created %d", n)
	}
	rows := reminderRows(t, f)
	if len(rows) != 1 {
		t.Fatalf("rows %d", len(rows))
	}
	return rows[0]
}

func graphWorker(t *testing.T, f grc01DServiceFixture, srv *httptest.Server, timeout time.Duration) *ReminderWorker {
	t.Helper()
	worker, err := NewReminderWorker(f.svc, newTestGraphTransport(t, srv, timeout), DefaultReminderWindows(), ReminderLeaseDefault, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	return worker
}

func newTestGraphTransport(t *testing.T, srv *httptest.Server, timeout time.Duration) *microsoft365.Transport {
	t.Helper()
	cfg := microsoft365.Config{
		TenantID:     "11111111-1111-1111-1111-111111111111",
		ClientID:     "local-test-client",
		ClientSecret: "local-test-secret",
		Sender:       "noreply@example.com",
		GraphBaseURL: srv.URL,
		TokenURL:     srv.URL + "/token",
		HTTPClient:   &http.Client{Timeout: timeout},
	}
	transport, err := microsoft365.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return transport
}

func graphStatusServer(t *testing.T, tokenStatus, sendStatus int, sends *atomic.Int32) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/token":
			if tokenStatus != http.StatusOK {
				w.WriteHeader(tokenStatus)
				_, _ = io.WriteString(w, `{"error":"invalid_client"}`)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"access_token":"local-test-token","expires_in":3600,"token_type":"Bearer"}`)
		case strings.HasSuffix(r.URL.Path, "/sendMail"):
			sends.Add(1)
			if sendStatus == http.StatusTooManyRequests {
				w.Header().Set("Retry-After", "30")
			}
			w.WriteHeader(sendStatus)
			_, _ = io.WriteString(w, `{"error":{"code":"ErrorInternal","message":"hidden"}}`)
		default:
			http.NotFound(w, r)
		}
	}))
}

func assertReminder(t *testing.T, f grc01DServiceFixture, id uint, status string, attempts int, last string) {
	t.Helper()
	var stored GRCReminderIntent
	if err := f.db.First(&stored, id).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Status != status || stored.AttemptCount != attempts || stored.LastError == nil || *stored.LastError != last {
		t.Fatalf("intent status=%s attempts=%d err=%v", stored.Status, stored.AttemptCount, stored.LastError)
	}
}
