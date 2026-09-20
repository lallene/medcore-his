// Package microsoft365 implements email.Transport via Microsoft Graph sendMail
// (LOT 26E).
//
// Deployment notes (concise):
//   - Application permission Mail.Send + admin consent required
//   - Dedicated sender mailbox recommended; restrict app mailbox access via
//     Exchange application authorization mechanisms
//   - Client-secret credentials are bootstrap/dev only; production should prefer
//     certificate or federated identity (TokenSource boundary supports that later)
//   - HTTP 202 means Graph accepted the request, not that the recipient received it
//   - No Graph sendMail idempotency / exactly-once (LOT 26H-1)
//   - Stable message correlation: x-medcore-notification-intent-id from
//     notification-intent:<id> IdempotencyKey (observability only)
//   - Per-attempt client-request-id UUID; response request-id for ops correlation
//   - Post-dispatch uncertain Do errors → email.ErrAmbiguousDelivery (LOT 26H-3)
//
// This package does not wire the notification worker (LOT 26F), templates (26G),
// or Retry-After scheduling (26H-4).
package microsoft365
