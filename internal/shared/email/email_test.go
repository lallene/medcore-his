package email_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/lallene/medcore-his/backend/internal/shared/email"
)

func validTextMessage() email.Message {
	return email.Message{
		To:             email.Address{Address: "patient@example.com", DisplayName: "Demo"},
		Subject:        "Rappel de rendez-vous",
		TextBody:       "Veuillez consulter MedCore HIS pour les détails.",
		IdempotencyKey: "intent-42-attempt-1",
	}
}

func TestFakeSuccessAndRecord(t *testing.T) {
	f := email.NewFake()
	f.NextProviderMessageID = "msg-1"
	msg := validTextMessage()

	res, err := f.Send(context.Background(), msg)
	if err != nil {
		t.Fatal(err)
	}
	if res.ProviderMessageID != "msg-1" {
		t.Fatalf("ProviderMessageID=%q", res.ProviderMessageID)
	}
	if f.ProviderName() != "fake" {
		t.Fatalf("ProviderName=%q", f.ProviderName())
	}
	sent := f.Sent()
	if len(sent) != 1 {
		t.Fatalf("sent=%d", len(sent))
	}
	if sent[0].To.Address != msg.To.Address || sent[0].Subject != msg.Subject ||
		sent[0].TextBody != msg.TextBody || sent[0].IdempotencyKey != msg.IdempotencyKey {
		t.Fatalf("recorded message mismatch: %+v", sent[0])
	}
}

func TestFakeDefaultProviderMessageID(t *testing.T) {
	f := email.NewFake()
	res, err := f.Send(context.Background(), validTextMessage())
	if err != nil {
		t.Fatal(err)
	}
	if res.ProviderMessageID != "fake" {
		t.Fatalf("ProviderMessageID=%q", res.ProviderMessageID)
	}
}

func TestErrorClassification(t *testing.T) {
	cause := errors.New("upstream")

	cases := []struct {
		name   string
		err    error
		target error
	}{
		{"not_configured", email.NotConfigured(nil), email.ErrNotConfigured},
		{"not_configured_wrap", email.NotConfigured(cause), email.ErrNotConfigured},
		{"transient", email.Transient(cause), email.ErrTransient},
		{"transient_nil", email.Transient(nil), email.ErrTransient},
		{"permanent", email.Permanent(cause), email.ErrPermanent},
		{"permanent_nil", email.Permanent(nil), email.ErrPermanent},
		{"ambiguous", email.AmbiguousDelivery(cause), email.ErrAmbiguousDelivery},
		{"ambiguous_nil", email.AmbiguousDelivery(nil), email.ErrAmbiguousDelivery},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !errors.Is(tc.err, tc.target) {
				t.Fatalf("errors.Is(%v, %v)=false", tc.err, tc.target)
			}
		})
	}

	var wrapped error = email.Transient(cause)
	if !errors.Is(wrapped, cause) {
		t.Fatal("transient should unwrap cause")
	}
	if errors.Is(email.Transient(cause), email.ErrPermanent) {
		t.Fatal("transient must not match permanent")
	}
	if errors.Is(email.Permanent(cause), email.ErrTransient) {
		t.Fatal("permanent must not match transient")
	}
	if errors.Is(email.ErrNotConfigured, email.ErrTransient) {
		t.Fatal("not configured must not match transient")
	}
	if errors.Is(email.AmbiguousDelivery(cause), email.ErrTransient) {
		t.Fatal("ambiguous must not match transient")
	}
	if errors.Is(email.Transient(cause), email.ErrAmbiguousDelivery) {
		t.Fatal("transient must not match ambiguous")
	}
	if errors.Is(email.AmbiguousDelivery(cause), email.ErrPermanent) {
		t.Fatal("ambiguous must not match permanent")
	}
	if strings.Contains(strings.ToLower(email.ErrAmbiguousDelivery.Error()), "patient") ||
		strings.Contains(email.ErrAmbiguousDelivery.Error(), "@") {
		t.Fatalf("sentinel must stay privacy-safe: %q", email.ErrAmbiguousDelivery.Error())
	}
}

func TestTransientRetryAfterContract26H4(t *testing.T) {
	cause := errors.New("throttle")
	err := email.TransientRetryAfter(cause, 2*time.Minute)
	if !errors.Is(err, email.ErrTransient) {
		t.Fatalf("want ErrTransient, got %v", err)
	}
	if !errors.Is(err, cause) {
		t.Fatal("cause must unwrap")
	}
	if errors.Is(err, email.ErrAmbiguousDelivery) {
		t.Fatal("retry-after must not be ambiguous")
	}
	d, ok := email.RetryAfter(err)
	if !ok || d != 2*time.Minute {
		t.Fatalf("RetryAfter=%v ok=%v", d, ok)
	}
	if strings.Contains(err.Error(), "Retry-After") || strings.Contains(err.Error(), "120") {
		t.Fatalf("error text must not dump retry header: %q", err.Error())
	}

	plain := email.Transient(cause)
	if _, ok := email.RetryAfter(plain); ok {
		t.Fatal("plain transient must have no hint")
	}
	if _, ok := email.RetryAfter(email.TransientRetryAfter(cause, 0)); ok {
		t.Fatal("zero hint must be absent")
	}
	if _, ok := email.RetryAfter(email.TransientRetryAfter(cause, -time.Second)); ok {
		t.Fatal("negative hint must be absent")
	}
	if _, ok := email.RetryAfter(email.AmbiguousDelivery(nil)); ok {
		t.Fatal("ambiguous must have no retry hint")
	}

	nilCause := email.TransientRetryAfter(nil, time.Minute)
	if !errors.Is(nilCause, email.ErrTransient) {
		t.Fatalf("nil cause must remain ErrTransient: %v", nilCause)
	}
	if d, ok := email.RetryAfter(nilCause); !ok || d != time.Minute {
		t.Fatalf("nil-cause hint=%v ok=%v", d, ok)
	}
	if nilCause == nil {
		t.Fatal("TransientRetryAfter must not return nil")
	}

	wrapped := fmt.Errorf("outer: %w", email.TransientRetryAfter(cause, 90*time.Second))
	if !errors.Is(wrapped, email.ErrTransient) {
		t.Fatal("wrapped must remain transient")
	}
	d, ok = email.RetryAfter(wrapped)
	if !ok || d != 90*time.Second {
		t.Fatalf("wrapped RetryAfter=%v ok=%v", d, ok)
	}
}

func TestFakeConfiguredErrors(t *testing.T) {
	f := email.NewFake()
	f.Err = email.Transient(errors.New("429"))
	_, err := f.Send(context.Background(), validTextMessage())
	if !errors.Is(err, email.ErrTransient) {
		t.Fatalf("want transient got %v", err)
	}
	if len(f.Sent()) != 0 {
		t.Fatal("failed send must not record")
	}

	f.Reset()
	f.Err = email.Permanent(errors.New("invalid mailbox"))
	_, err = f.Send(context.Background(), validTextMessage())
	if !errors.Is(err, email.ErrPermanent) {
		t.Fatalf("want permanent got %v", err)
	}

	f.Reset()
	f.Err = email.NotConfigured(nil)
	_, err = f.Send(context.Background(), validTextMessage())
	if !errors.Is(err, email.ErrNotConfigured) {
		t.Fatalf("want not configured got %v", err)
	}
}

func TestFakeContextCancellation(t *testing.T) {
	f := email.NewFake()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := f.Send(ctx, validTextMessage())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled got %v", err)
	}
	if len(f.Sent()) != 0 {
		t.Fatal("cancelled send must not record")
	}
}

func TestValidateFailures(t *testing.T) {
	base := validTextMessage()
	cases := []struct {
		name string
		msg  email.Message
	}{
		{"empty_to", func() email.Message { m := base; m.To.Address = ""; return m }()},
		{"bad_to", func() email.Message { m := base; m.To.Address = "not-an-email"; return m }()},
		{"mailbox_form", func() email.Message {
			m := base
			m.To = email.Address{Address: "John <john@example.com>"}
			return m
		}()},
		{"quoted_mailbox_form", func() email.Message {
			m := base
			m.To = email.Address{Address: `"John Doe" <john@example.com>`}
			return m
		}()},
		{"empty_subject", func() email.Message { m := base; m.Subject = "  "; return m }()},
		{"empty_body", func() email.Message { m := base; m.TextBody = ""; m.HTMLBody = ""; return m }()},
		{"whitespace_bodies", func() email.Message {
			m := base
			m.TextBody = "  "
			m.HTMLBody = "\t\n"
			return m
		}()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.msg.Validate(); !errors.Is(err, email.ErrInvalidMessage) {
				t.Fatalf("Validate: %v", err)
			}
			f := email.NewFake()
			if _, err := f.Send(context.Background(), tc.msg); !errors.Is(err, email.ErrInvalidMessage) {
				t.Fatalf("Send: %v", err)
			}
			if len(f.Sent()) != 0 {
				t.Fatal("invalid message must not record")
			}
		})
	}
}

func TestValidAddrSpecWithDisplayNameAndWhitespace(t *testing.T) {
	msg := email.Message{
		To: email.Address{
			Address:     " john@example.com ",
			DisplayName: "John Doe",
		},
		Subject:  "S",
		TextBody: "hello",
	}
	if err := msg.Validate(); err != nil {
		t.Fatal(err)
	}
	// Validate must not mutate Message.
	if msg.To.Address != " john@example.com " || msg.To.DisplayName != "John Doe" {
		t.Fatalf("mutated: %+v", msg.To)
	}
}

func TestValidTextOnlyAndHTML(t *testing.T) {
	textOnly := email.Message{
		To:       email.Address{Address: "a@b.co"},
		Subject:  "S",
		TextBody: "hello",
	}
	if err := textOnly.Validate(); err != nil {
		t.Fatal(err)
	}

	htmlOnly := email.Message{
		To:       email.Address{Address: "a@b.co"},
		Subject:  "S",
		HTMLBody: "<p>hello</p>",
	}
	if err := htmlOnly.Validate(); err != nil {
		t.Fatal(err)
	}

	both := email.Message{
		To:       email.Address{Address: "a@b.co", DisplayName: "A"},
		Subject:  "S",
		TextBody: "hello",
		HTMLBody: "<p>hello</p>",
	}
	f := email.NewFake()
	if _, err := f.Send(context.Background(), both); err != nil {
		t.Fatal(err)
	}
	if len(f.Sent()) != 1 || f.Sent()[0].HTMLBody == "" || f.Sent()[0].TextBody == "" {
		t.Fatalf("recorded=%+v", f.Sent())
	}
}

func TestFakeSentDefensiveCopy(t *testing.T) {
	f := email.NewFake()
	msg := validTextMessage()
	if _, err := f.Send(context.Background(), msg); err != nil {
		t.Fatal(err)
	}
	got := f.Sent()
	if len(got) != 1 {
		t.Fatalf("sent=%d", len(got))
	}
	got[0].Subject = "MUTATED"
	got[0].TextBody = "MUTATED"
	again := f.Sent()
	if again[0].Subject != msg.Subject || again[0].TextBody != msg.TextBody {
		t.Fatalf("internal record mutated via Sent() copy: %+v", again[0])
	}
}

func TestFakeImplementsTransport(t *testing.T) {
	var _ email.Transport = email.NewFake()
}
