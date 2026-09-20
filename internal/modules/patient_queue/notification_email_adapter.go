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
// Appointment email copy is owned by NotificationEmailRenderer (not this adapter).
type EmailDeliveryAdapter struct {
	transport email.Transport
	patients  PatientEmailReader
	renderer  NotificationEmailRenderer
}

// NewEmailDeliveryAdapter wires transport, patient email reader, and content renderer.
func NewEmailDeliveryAdapter(
	transport email.Transport,
	patients PatientEmailReader,
	renderer NotificationEmailRenderer,
) *EmailDeliveryAdapter {
	if transport == nil {
		panic("email delivery adapter: transport required")
	}
	if patients == nil {
		panic("email delivery adapter: patient email reader required")
	}
	if renderer == nil {
		panic("email delivery adapter: notification email renderer required")
	}
	return &EmailDeliveryAdapter{transport: transport, patients: patients, renderer: renderer}
}

func (a *EmailDeliveryAdapter) Channel() string { return NotifChannelEmail }

func (a *EmailDeliveryAdapter) ProviderName() string {
	return a.transport.ProviderName()
}

// Send resolves the patient recipient, renders appointment copy, and calls Transport.Send.
// Missing/invalid recipients return Skipped without calling the renderer or transport.
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

	rendered, err := a.renderer.Render(intent.Kind, payload)
	if err != nil {
		// Preserve renderer error identity (e.g. email.ErrInvalidMessage → terminal).
		return DeliveryResult{}, err
	}

	msg := email.Message{
		To:             email.Address{Address: to},
		Subject:        rendered.Subject,
		TextBody:       rendered.TextBody,
		HTMLBody:       "",
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
