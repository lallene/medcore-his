// Package email defines a provider-neutral outbound email transport boundary.
//
// Architecture (LOT 26D):
//
//	business event
//	  → durable notification intent (patient_queue / 23N)
//	  → NotificationDeliveryAdapter (channel boundary)
//	  → email channel adapter (future LOT 26F)
//	  → email.Transport (this package)
//	  → concrete provider (future LOT 26E, e.g. Microsoft 365)
//
// This package accepts only Message values. Callers are responsible for producing
// privacy-approved outbound copy (neutral event, schedule, organization cues;
// clinical details stay inside MedCore HIS — LOT 26G).
//
// Scope split:
//   - 26D: provider-neutral Message / Transport / errors / fake (this package)
//   - 26E: Microsoft 365 (or other) Transport implementation
//   - 26F: wire EMAIL channel into the durable notification worker
//   - 26G: templates and PHI/privacy policy for outbound copy
//   - 26H: reliability — at-least-once external delivery; ErrAmbiguousDelivery is
//     terminal (no blind retry). IdempotencyKey is correlation only; Graph sendMail
//     has no provider idempotency primitive (LOT 26H-1).
//
// IdempotencyKey is correlation/deduplication hint only. It does not guarantee
// exactly-once delivery.
package email
