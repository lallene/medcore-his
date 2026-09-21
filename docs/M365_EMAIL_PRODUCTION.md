# Microsoft 365 email — production security & runbook (LOT 26I-6)

Authoritative production-security contract and operator runbook for MedCore
appointment **EMAIL** delivery through Microsoft Graph.

Related:

- Architecture / enablement: [MEDICAL_SCHEDULING.md](./MEDICAL_SCHEDULING.md)
- Worker observability / alerts / ambiguous delivery: [NOTIFICATION_WORKER_OPERATIONS.md](./NOTIFICATION_WORKER_OPERATIONS.md)
- Implementation source of truth: `internal/shared/email/microsoft365/`,
  `cmd/notification-worker/adapters.go`

This document does **not** change authentication code. It documents the
**current implemented baseline** and Microsoft-recommended **future hardening**.

---

## 1. Purpose / scope

**In scope**

- Production provisioning of Entra application identity + Graph/Exchange
  authorization for MedCore notification-worker email
- Runtime configuration and secret-delivery contract
- Operator procedures: smoke test, rotation, compromise, outage, flag drift,
  decommission
- Privacy and least-privilege requirements

**Out of scope**

- Redesigning Go authentication (certificate / managed identity / federation)
- Inventing Vault/Kubernetes/Azure Key Vault as the deployment platform
- Deploying Prometheus/Grafana/Alertmanager
- Expanding email templates
- LOT **26J**

---

## 2. Current MedCore architecture

| Concern | Current implementation |
|---------|------------------------|
| Auth grant | OAuth2 **`client_credentials`** |
| Token authority | `https://login.microsoftonline.com/{TenantID}/oauth2/v2.0/token` |
| Scope | `https://graph.microsoft.com/.default` |
| Graph call | `POST /v1.0/users/{sender}/sendMail` |
| Success | HTTP **202 Accepted** (Graph accepted the request — not proof of recipient delivery) |
| Sender | `MEDCORE_M365_SENDER` only (URL path; not JSON `from`) |
| Recipient | Delivery-time `patients.email` only |
| Credentials process | **notification-worker only** |
| API | `MEDCORE_NOTIFICATION_EMAIL_ENABLED` for enqueue only — **no** M365 secret |
| migrate | **no** M365 credentials |
| Graph in readiness | **No** — `/readyz` is worker + DB only |

Token acquisition and Graph HTTP occur lazily on first `Send` after worker
startup constructs the transport.

---

## 3. Security invariants

1. **Sender authority** is configuration-only (`MEDCORE_M365_SENDER`).
2. Notification payload, renderer, and patient data **cannot** choose From.
3. **Client secret** never appears in logs, metrics, health, tickets, images, or git.
4. Access tokens are **memory-only** (no DB/disk persistence).
5. Application logs use bounded `operation` / `error_class` — never raw Graph
   bodies, tokens, secrets, or recipient content.
6. Metrics labels stay bounded (`channel`, `provider`, `outcome`, `result`).
7. Untrusted payload cannot pick an arbitrary sender mailbox.
8. **Ambiguous** Graph outcomes are terminal — **no blind replay**.
9. Mailbox send authority must be **effectively scoped** before production
   EMAIL activation (see §5 and §14).
10. API and worker EMAIL enablement flags must stay **aligned**.

---

## 4. Microsoft Graph permission model

MedCore needs **send only**.

| Item | Requirement |
|------|-------------|
| Graph API | `POST /users/{id \| userPrincipalName}/sendMail` |
| Least-privileged application permission (Graph) | **`Mail.Send`** |
| Admin consent | **Required** for application `Mail.Send` |

**Do not** grant for MedCore email sending:

- `Mail.ReadWrite`
- `Directory.ReadWrite.All`
- `User.ReadWrite.All`
- other read/write directory or mailbox permissions “just in case”

Application `Mail.Send` **without** effective mailbox scoping can authorize
sending as **any** mailbox in the tenant. Mailbox scoping is therefore a
**production security requirement**, not an optional nicety.

---

## 5. Exchange mailbox authorization / scoping

### 5.1 Preferred model (new configurations)

Current Microsoft guidance for **new** Exchange resource-scoped application
authorization:

**Exchange Online — Role Based Access Control (RBAC) for Applications**

Conceptual production shape:

1. Microsoft Entra **service principal** for the MedCore worker identity
2. Exchange Online **Application Mail.Send** role assignment for that principal
3. **Resource scope** limited to the intended sender mailbox (or tightly
   bounded mailbox set)

Prefer a **conceptual admin checklist** executed against current Microsoft
Learn. Do **not** treat fragile PowerShell snippets in MedCore docs as the
source of truth — always verify commands against:

https://learn.microsoft.com/en-us/exchange/permissions-exo/application-rbac

### 5.2 Legacy Application Access Policies

**Application Access Policies** can constrain certain Entra application
permissions (including `Mail.Send`), but Microsoft marks this mechanism
**legacy** and recommends **RBAC for Applications** for new access
configuration.

**Do not** recommend `New-ApplicationAccessPolicy` for a **fresh** MedCore
deployment. Legacy policies may appear only as migration context for tenants
that already use them.

Reference:

https://learn.microsoft.com/en-us/exchange/permissions-exo/application-access-policies

### 5.3 Additive permission warning — CRITICAL

> **DO NOT** assume a scoped Exchange RBAC assignment overrides an
> organization-wide Microsoft Entra `Mail.Send` application grant.
>
> Microsoft Entra application permission grants and Exchange Online RBAC for
> Applications grants are **additive** (union). Each authority can act
> independently.
>
> If organization-wide Entra `Mail.Send` remains granted while a scoped
> Exchange **Application Mail.Send** role is also assigned, the **unscoped
> Entra permission can still provide organization-wide send authority**.

For a resource-scoped design, operators must:

1. Configure the intended Exchange RBAC scope
2. Review and **remove unneeded organization-wide Entra grants** per current
   Microsoft guidance
3. Verify **effective** access before production activation (§14)

Microsoft states this explicitly in the RBAC for Applications FAQ
(“Why does my application still have access to mailboxes that aren't granted
by the scope…”).

---

## 6. Authentication credential posture

### A. CURRENT IMPLEMENTED AUTHENTICATION MODE

MedCore **currently implements**:

- OAuth2 `client_credentials`
- `MEDCORE_M365_CLIENT_ID`
- `MEDCORE_M365_CLIENT_SECRET`

This is the **current implemented baseline**. It is **not** claimed to be
Microsoft’s preferred long-term production credential type.

While secret mode remains in use, operators **must**:

- Store the secret outside source and image
- Inject at worker runtime only
- Restrict secret access to the notification-worker runtime
- Set a defined secret expiration
- Rotate **before** expiration
- Never copy the secret into tickets or logs
- Revoke immediately on suspected compromise

### B. MICROSOFT-RECOMMENDED PRODUCTION HARDENING (FUTURE)

Microsoft identity platform guidance prefers stronger credentials than
shared client secrets for production confidential clients, typically in this
order depending on hosting:

1. **Managed identity** (when the workload runs on Azure and MI applies)
2. **Workload / federated identity** (when federation applies)
3. **Certificate credential**
4. **Client secret** only as a weaker compatibility / current-implementation mode

**MedCore Go does not yet implement** managed identity, federation, or
certificate `TokenSource` authentication. Supporting those modes requires a
separate, tested code change. Migration away from shared-secret auth is
**production hardening**, not part of this documentation LOT’s code work.

References:

- https://learn.microsoft.com/en-us/entra/identity-platform/how-to-add-credentials
- https://learn.microsoft.com/en-us/entra/identity-platform/security-best-practices-for-app-registration
- https://learn.microsoft.com/en-us/entra/identity-platform/certificate-credentials

---

## 7. Environment / configuration contract

| Variable | Purpose | Required when | Classification | Startup if missing/invalid |
|----------|---------|---------------|----------------|----------------------------|
| `MEDCORE_NOTIFICATION_EMAIL_ENABLED` | Shared API enqueue + worker EMAIL adapter registration | Always parsed; empty → disabled | Non-secret bool | Invalid explicit value → config fatal |
| `MEDCORE_M365_TENANT_ID` | Tenant-specific token authority | EMAIL enabled on **worker** | Operational ID | Worker fail-closed |
| `MEDCORE_M365_CLIENT_ID` | Application client ID | Same | Operational ID | Worker fail-closed |
| `MEDCORE_M365_CLIENT_SECRET` | Client secret for token grant | Same | **SECRET** | Worker fail-closed |
| `MEDCORE_M365_SENDER` | Graph sendMail mailbox (addr-spec / UPN) | Same | Operational mailbox | Worker fail-closed |

Notes:

- Tenant must be an explicit GUID or DNS-like tenant domain — not
  `common` / `organizations` / `consumers` (rejected by validation).
- Sender must be a single addr-spec (no display-name form).
- When EMAIL is **disabled**, M365 env is ignored (LOG-only worker).
- API does **not** require M365 vars when the flag is true.
- Never log secret **values**. Naming the env **key** in a startup error is OK.

---

## 8. Secret-management contract

`MEDCORE_M365_CLIENT_SECRET` must be:

- Injected at **runtime** only
- Available to the **notification-worker** only
- **Never** committed to the repository
- **Never** baked into the container image
- **Never** passed as a Docker **build ARG**
- **Never** required merely to **build** the CI image
- **Never** exposed via metrics, `/healthz`, `/readyz`, application logs, or
  support tickets

Use the **platform’s approved secret-management mechanism**. This repository
does not establish Vault, Kubernetes Secrets, Azure Key Vault, or any other
specific secret store as mandatory.

Images: see `Dockerfile` — no M365 `ARG`/`ENV` with credentials; secrets are
runtime configuration.

---

## 9. Production provisioning checklist

Ordered operator checklist (platform-neutral):

1. Register / dedicate a MedCore Entra application identity for the worker.
2. Configure **tenant-specific** authority (explicit tenant ID/domain).
3. Configure **only** required email authorization (`Mail.Send` / Exchange
   **Application Mail.Send** as appropriate — no unrelated Graph perms).
4. Configure **effective** mailbox scope via Exchange RBAC for Applications
   (preferred for new setups).
5. Verify the intended sender mailbox is **in** scope (authorized send).
6. Verify an **unrelated** mailbox is **outside** scope (unauthorized send
   fails).
7. Review additive grants: remove unneeded **organization-wide** Entra
   `Mail.Send` if relying on scoped Exchange RBAC (§5.3).
8. Configure runtime worker environment (`MEDCORE_M365_*`, EMAIL flag).
9. Inject credential via approved secret mechanism.
10. Keep **API and worker** `MEDCORE_NOTIFICATION_EMAIL_ENABLED` **aligned**.
11. Run the **migration** job first (LOT 26I-3).
12. Deploy API / notification-worker per the deployment plan.
13. Verify `/healthz`.
14. Verify `/readyz`.
15. Verify `/metrics` scrape (private network).
16. Perform controlled **non-clinical** smoke test (§11).
17. Verify durable notification intent/attempt result.
18. Verify delivery/provider metrics (`channel="email"`, `provider="microsoft365"`).
19. Record operational verification **without** PHI or secrets.

Do **not** declare least privilege complete until steps 5–7 are
admin-verified.

---

## 10. Deployment order

Preserve LOT **26I-3**:

1. **Migration job** succeeds (sole schema owner: `cmd/migrate`)
2. Then API / notification-worker rollout

API and worker do **not** own schema migration.

M365 credentials: **notification-worker only**.

---

## 11. Controlled smoke-test procedure

There is **no** MedCore test-send / debug-send HTTP endpoint. Do **not** invent
one for this LOT.

### Preferred: MedCore end-to-end (wiring validation)

Use a controlled **non-clinical** fixture/workflow with an **operator-owned**
test mailbox as `patients.email`:

- No real patient PHI
- No clinical content in templates (current renderer is lifecycle + datetime only)
- Known test recipient
- Known expected sender (`MEDCORE_M365_SENDER`)
- Verify durable intent + attempt outcome via approved admin read API
- Verify provider metrics / delivery outcomes
- Do not put recipient addresses, tokens, or secrets into tickets/logs

### Complementary: external Graph/Exchange authorization tests

Administrators may validate Graph/Exchange authorization using approved
Microsoft administration tooling (including Exchange
`Test-ServicePrincipalAuthorization` where applicable).

External Graph testing **does not replace** MedCore end-to-end smoke when
validating application wiring (flag, env, worker adapters, durable finalize).

---

## 12. Health / readiness verification

| Endpoint | Role |
|----------|------|
| `GET /healthz` | Worker process / run-loop lifecycle |
| `GET /readyz` | Worker live + bounded DB `PingContext` |
| `GET /metrics` | In-memory Prometheus exposition |

**Microsoft Graph is not part of readiness.** A Graph outage or token failure
must surface through **delivery/provider metrics**, durable attempts, and
alerts — not by marking the worker unready and causing restart loops while the
process and DB remain healthy.

Invalid M365 configuration when EMAIL is enabled still fails at **startup**
(fail-closed).

---

## 13. Observability verification

After smoke / under load, operators should confirm (see 5F ops doc):

- `medcore_notification_delivery_attempts_total{channel="email",...}`
- `medcore_notification_provider_duration_seconds{channel="email",provider="microsoft365"}`
- Queue gauges with `channel="email"` using **`max by (channel)`** — never
  `sum` across replicas

Prometheus/Grafana deployment is **external** and not proven by this
repository.

---

## 14. Mailbox scope verification

Before production EMAIL activation, admin-verify:

| Test | Expected |
|------|----------|
| **Authorized** mailbox | Application can send as `MEDCORE_M365_SENDER` |
| **Unauthorized** mailbox | Application **cannot** send as another mailbox outside intended scope |

Do **not** store mailbox addresses in application logs. Record pass/fail only
in operational change records without secrets.

Until both tests pass, least privilege is **not** complete — even if Entra
shows `Mail.Send` consented.

---

## 15. Credential rotation (current secret mode)

While client-secret authentication remains the implemented mode:

1. Create a **replacement** secret using the approved Entra admin process
2. Inject the new secret into the worker runtime secret mechanism
3. **Restart / roll** the notification-worker (token source / config is built
   at startup)
4. Controlled smoke test
5. Verify telemetry (no sustained auth permanent failures)
6. **Revoke** the old secret
7. Verify the old secret is no longer usable via approved admin verification

Never print the credential. Do not assume a specific orchestrator.

---

## 16. Credential / app compromise procedure

1. Stop or disable EMAIL delivery if necessary (align API + worker flags, or
   stop the worker)
2. **Revoke** the compromised credential and/or application access in Entra /
   Exchange as appropriate
3. Preserve durable notification audit (intents/attempts)
4. Inspect **bounded** metrics and application logs only (`operation`,
   `error_class`, delivery outcomes)
5. Re-verify mailbox authorization scope (§14)
6. Issue a replacement credential if continuing secret mode
7. Roll the worker with the new secret
8. Controlled smoke test
9. Restore EMAIL enablement when safe
10. Monitor EMAIL due backlog drain via queue gauges / 5F alerts

Never paste secrets or tokens into incident tickets.

---

## 17. Microsoft 365 outage procedure

Map current MedCore behavior (do not invent automatic recovery). Graph HTTP
taxonomy matches `microsoft365` transport classification (LOT 26H unchanged):

| Condition | Classification | Operator focus |
|-----------|----------------|----------------|
| HTTP **400 / 401 / 403 / 404 / 409** | **Permanent** | Config, consent, mailbox scope, secret validity, message/permission defects |
| HTTP **408** | Transient; retry | Connectivity / timeouts |
| HTTP **429** | Transient; **Retry-After** delay-seconds floor when valid; retry | Transient rate; due age; provider latency |
| HTTP **5xx** | Transient; retry | Same |
| Pre-dispatch network / timeout | Transient; retry | Same; DB/worker health |
| Post-dispatch **ambiguous** | Terminal **FAILED**; **no blind retry** | 5F ambiguous runbook |
| Worker unavailable | No claims; queue ages | `up`, `/healthz`, `/readyz` |

Transient retries follow existing MedCore backoff; **attempt 5 is terminal**
(`NotificationMaxAttempts` = 5). Ambiguous outcomes are terminal even before
max attempts.

Alert / dashboard mapping: [NOTIFICATION_WORKER_OPERATIONS.md](./NOTIFICATION_WORKER_OPERATIONS.md).
Thresholds are operational defaults, **not** SLAs.

---

## 18. EMAIL flag drift procedure

**API EMAIL enabled** + **worker EMAIL disabled**:

- API enqueues durable `EMAIL` intents
- Worker claim filter = registered channels only → EMAIL is **never claimed**
- Intents remain **`PENDING` / due**
- This does **not** necessarily emit `adapter_unavailable` or `not_configured`
  (those require a claimed EMAIL path)

Operators must watch:

```promql
max by (channel) (medcore_notification_queue_due{channel="email"})
max by (channel) (medcore_notification_queue_oldest_due_age_seconds{channel="email"})
```

and the corresponding 5F queue-age alerts.

**Fix:** align `MEDCORE_NOTIFICATION_EMAIL_ENABLED` on API and worker, then
confirm due backlog drains.

Production checklist must verify flag alignment before go-live.

---

## 19. Ambiguous-delivery procedure

Do **not** duplicate the full 5F runbook here.

**Authoritative procedure:**
[NOTIFICATION_WORKER_OPERATIONS.md](./NOTIFICATION_WORKER_OPERATIONS.md)
§ Ambiguous delivery runbook.

Summary only:

- Outcome may mean **possible provider acceptance**
- **No** blind replay / re-send
- Inspect durable intent + attempt history via approved admin API
- Verify external provider state when available
- Escalate unresolved ambiguity as delivery-integrity
- Preserve the known **26H** residual accept-before-finalize window

---

## 20. Privacy requirements

### Application email content (current renderer)

- French
- Plain text
- Appointment lifecycle phrasing only
- Civil date/time in business timezone

**Must not** appear in email copy:

- Clinical reason, diagnosis, prescription
- Lab / imaging results
- Patient phone or other medical details

Do not expand templates in 26I-6.

### Operational privacy

Forbidden in metrics labels, alert annotations, tickets, and routine logs:

- Patient / appointment / intent / attempt IDs (application logs)
- Recipient email, subject, body, payload
- Tokens, secrets, DSN
- Raw Graph request/response bodies

---

## 21. Admin diagnostics

Use the existing **read-only** admin notification API (permissions such as
`schedule.manage.service` / `schedule.manage.all` — see scheduling docs).

Inspect:

- Intent state
- Attempt history
- Sanitized provider diagnostic text (status / safe code / optional request-id)
- Optional `providerMessageId` when present

Do **not** expect recipient address or message body on notification diagnostic
DTOs. Prefer this API over raw DB dumps for normal operations.

There is no admin “send” / “retry” mutation endpoint in current MedCore.

---

## 22. Graph error privacy

Current sanitization boundary for provider errors:

**May appear** (bounded): HTTP status, safe provider error **code**, optional
safe Graph `request-id`.

**Must not** appear: Graph `message` / OAuth `error_description`, request body,
recipient, subject/body, access token, client secret.

Durable attempt `Error` (≤500 chars) remains **admin-visible** via the approved
read path — treat as operationally sensitive, not a free-text dump target.

---

## 23. Token handling contract

Current source behavior:

- Access token held **in process memory** only
- No DB / disk persistence
- No metrics / health / application-log exposure
- Refresh when within **60s** of expiry (`tokenRefreshSkew`)

**Known LOW:** concurrent cold refreshes may issue duplicate token requests
(fetch outside mutex). Not a production blocker.

---

## 24. Decommission / revocation

1. Disable EMAIL enqueue/delivery **coherently** (API + worker flags)
2. Drain or resolve intended pending EMAIL work per ops policy
3. Stop worker email use (flag off or stop worker)
4. Revoke application authorization (Entra + Exchange as applicable)
5. Revoke / remove credential material
6. Verify intended mailbox is no longer accessible to the former app identity
7. Retain durable audit per organizational retention policy

This document does **not** invent a retention duration.

---

## 25. Known limitations / future hardening

| Item | Status |
|------|--------|
| Client-secret authentication | **Current implemented mode** |
| Managed identity / federation / certificate auth | **Not implemented** — Microsoft-recommended future hardening |
| Token refresh stampede | LOW residual |
| API/worker EMAIL flag drift | Operational risk — monitor 5D gauges |
| Durable attempt diagnostic text | Known debt (bounded status/code/request-id) |
| 26H ambiguous external-send/finalize window | Residual — see 5F runbook |
| Prometheus/Grafana/alerts deployed | **Not proven** by this repo |
| Exchange mailbox restriction “done” | **Not claimed** until admin verifies §14 |

---

## 26. Microsoft authoritative references

Normative Microsoft Learn only (verify current revision before changing tenant
config):

| Topic | URL |
|-------|-----|
| Graph `user: sendMail` | https://learn.microsoft.com/en-us/graph/api/user-sendmail |
| OAuth2 client credentials | https://learn.microsoft.com/en-us/entra/identity-platform/v2-oauth2-client-creds-grant-flow |
| Add/manage app credentials | https://learn.microsoft.com/en-us/entra/identity-platform/how-to-add-credentials |
| App registration security best practices | https://learn.microsoft.com/en-us/entra/identity-platform/security-best-practices-for-app-registration |
| Certificate credentials | https://learn.microsoft.com/en-us/entra/identity-platform/certificate-credentials |
| Exchange RBAC for Applications | https://learn.microsoft.com/en-us/exchange/permissions-exo/application-rbac |
| Application Access Policies (legacy) | https://learn.microsoft.com/en-us/exchange/permissions-exo/application-access-policies |

Do not treat blogs, forums, or Q&A threads as normative security sources.
