package grc

import (
	"fmt"
	"strings"
	"time"

	"github.com/lallene/medcore-his/backend/internal/shared/email"
)

const (
	reminderSubjectEvidence = "GRC — Preuve arrivant à échéance"
	reminderSubjectAction   = "GRC — Action à traiter"
	reminderSubjectPolicy   = "GRC — Politique arrivant à échéance"

	reminderErrInactive      = "destinataire inactif"
	reminderErrNoEmail       = "destinataire sans adresse e-mail"
	reminderErrMissing       = "destinataire introuvable"
	reminderErrTargetMissing = "cible du rappel introuvable"
	reminderErrObsolete      = "rappel obsolète"
	reminderErrTransient     = "échec transitoire de livraison"
	reminderErrPermanent     = "échec permanent de livraison"
	reminderErrAmbiguous     = "issue de livraison ambiguë"
	reminderErrNotConfigured = "transport e-mail non configuré"
	reminderErrStale         = "bail de traitement expiré"
)

type reminderFacts struct {
	Reference string
	Title     string
	At        time.Time
	Stage     string
}

// renderReminderMessage builds the only GRC reminder copy. The subject is
// fixed. Reference and title are single-line. Descriptions, external
// references, and clinical fields are not parameters of this function.
func renderReminderMessage(to email.Address, intent GRCReminderIntent, facts reminderFacts) (email.Message, error) {
	when := facts.At.UTC().Format(time.RFC3339)
	ref := singleLine(facts.Reference)
	title := singleLine(facts.Title)
	var subject, body string
	switch intent.Kind {
	case ReminderKindEvidenceExpiring:
		subject = reminderSubjectEvidence
		body = fmt.Sprintf("Une preuve GRC dont vous êtes responsable arrive à échéance.\n\nRéférence: %s\nTitre: %s\nDate d'expiration: %s\n\nConsultez MedCore HIS pour plus de détails.\n", ref, title, when)
	case ReminderKindActionDue:
		subject = reminderSubjectAction
		body = fmt.Sprintf("Une action GRC qui vous est assignée arrive à échéance ou est échue.\n\nRéférence: %s\nTitre: %s\nÉchéance: %s\nÉtat d'échéance: %s\n\nConsultez MedCore HIS pour plus de détails.\n", ref, title, when, facts.Stage)
	case ReminderKindPolicyExpiring:
		subject = reminderSubjectPolicy
		body = fmt.Sprintf("Une politique GRC dont vous êtes responsable arrive à échéance.\n\nRéférence: %s\nTitre: %s\nDate de fin d'effet: %s\n\nConsultez MedCore HIS pour plus de détails.\n", ref, title, when)
	default:
		return email.Message{}, fmt.Errorf("kind de rappel inconnu")
	}
	msg := email.Message{
		To:             to,
		Subject:        subject,
		TextBody:       body,
		IdempotencyKey: fmt.Sprintf("grc-reminder:%d", intent.ID),
	}
	if err := msg.Validate(); err != nil {
		return email.Message{}, err
	}
	return msg, nil
}

func singleLine(value string) string {
	value = strings.ReplaceAll(value, "\r", " ")
	value = strings.ReplaceAll(value, "\n", " ")
	return strings.TrimSpace(value)
}

func clampReminderError(message string) string {
	message = singleLine(message)
	if message == "" {
		message = reminderErrTransient
	}
	runes := []rune(message)
	if len(runes) > 500 {
		return string(runes[:500])
	}
	return message
}
