package patient_queue

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/lallene/medcore-his/backend/internal/shared/email"
	"gorm.io/gorm"
)

// Safe skip reason for missing/invalid patient recipients (no PHI / no addresses).
const NotifSkipReasonRecipientUnavailable = "recipient unavailable"

// EmailDeliveryAdapter delivers appointment notification intents over email.Transport.
// Recipient is resolved at Send time from patients.email via PatientEmailReader.
type EmailDeliveryAdapter struct {
	transport email.Transport
	patients  PatientEmailReader
}

// NewEmailDeliveryAdapter wires a provider-neutral transport and patient email reader.
func NewEmailDeliveryAdapter(transport email.Transport, patients PatientEmailReader) *EmailDeliveryAdapter {
	if transport == nil {
		panic("email delivery adapter: transport required")
	}
	if patients == nil {
		panic("email delivery adapter: patient email reader required")
	}
	return &EmailDeliveryAdapter{transport: transport, patients: patients}
}

func (a *EmailDeliveryAdapter) Channel() string { return NotifChannelEmail }

func (a *EmailDeliveryAdapter) ProviderName() string {
	return a.transport.ProviderName()
}

// Send resolves the patient recipient, builds a minimal scheduling message, and calls Transport.Send.
// Missing/invalid recipients return Skipped without calling the transport.
func (a *EmailDeliveryAdapter) Send(ctx context.Context, intent *AppointmentNotificationIntent, payload NotificationPayload) (DeliveryResult, error) {
	if intent == nil {
		return DeliveryResult{Skipped: true, SkipReason: NotifSkipReasonRecipientUnavailable}, nil
	}

	raw, err := a.patients.FindPatientEmail(ctx, intent.PatientID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return DeliveryResult{Skipped: true, SkipReason: NotifSkipReasonRecipientUnavailable}, nil
		}
		// Unexpected lookup failure: surface as error for worker classification (26F-3).
		return DeliveryResult{}, err
	}

	to := strings.TrimSpace(raw)
	if to == "" || !emailRecipientAddrSpecOK(to) {
		return DeliveryResult{Skipped: true, SkipReason: NotifSkipReasonRecipientUnavailable}, nil
	}

	msg := email.Message{
		To:             email.Address{Address: to},
		Subject:        emailSubjectForKind(intent.Kind),
		TextBody:       emailTextBody(payload),
		IdempotencyKey: fmt.Sprintf("notification-intent:%d", intent.ID),
	}

	res, err := a.transport.Send(ctx, msg)
	if err != nil {
		return DeliveryResult{}, err
	}
	return DeliveryResult{ProviderMessageID: res.ProviderMessageID}, nil
}

// emailRecipientAddrSpecOK reuses shared/email Message.Validate address rules
// (trim, addr-spec only, no display-name) without calling Transport.Send.
func emailRecipientAddrSpecOK(addr string) bool {
	probe := email.Message{
		To:       email.Address{Address: addr},
		Subject:  ".",
		TextBody: ".",
	}
	return probe.Validate() == nil
}

func emailSubjectForKind(kind string) string {
	switch kind {
	case NotifKindBooked:
		return "MedCore — Appointment booked"
	case NotifKindRescheduled:
		return "MedCore — Appointment updated"
	case NotifKindCancelled:
		return "MedCore — Appointment cancelled"
	case NotifKindReminderT24H:
		return "MedCore — Appointment reminder"
	default:
		return "MedCore — Appointment notification"
	}
}

func emailTextBody(payload NotificationPayload) string {
	var b strings.Builder
	b.WriteString("Scheduled at: ")
	b.WriteString(strings.TrimSpace(payload.ScheduledAt))
	b.WriteByte('\n')
	if v := strings.TrimSpace(payload.AppointmentTypeName); v != "" {
		b.WriteString("Type: ")
		b.WriteString(v)
		b.WriteByte('\n')
	}
	if v := strings.TrimSpace(payload.ServiceName); v != "" {
		b.WriteString("Service: ")
		b.WriteString(v)
		b.WriteByte('\n')
	}
	if v := strings.TrimSpace(payload.ClinicLabel); v != "" {
		b.WriteString("Clinic: ")
		b.WriteString(v)
		b.WriteByte('\n')
	}
	return b.String()
}
