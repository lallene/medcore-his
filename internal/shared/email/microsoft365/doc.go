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
//   - No exactly-once / deduplication guarantee (see LOT 26H)
//
// This package does not wire the notification worker (LOT 26F), templates (26G),
// or retry scheduling (26H).
package microsoft365
