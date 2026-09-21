# Notification worker — observability operations (LOT 26I-5F)

Production operations contract for `cmd/notification-worker`: Prometheus scrape,
multi-replica aggregation, dashboard PromQL, alert recommendations, and
operator runbooks.

This document defines the **contract**. It does **not** ship Prometheus,
Grafana, Alertmanager, or Kubernetes monitoring manifests — the repository
has no established monitoring deployment convention. Deployments must
implement this contract in their platform scrape/alerting stack.

Source of truth for metric names/labels: `cmd/notification-worker/metrics.go`.
Related architecture: [MEDICAL_SCHEDULING.md](./MEDICAL_SCHEDULING.md)
(lots 26I-5A…5E).

---

## 1. Metric inventory

| Metric | Type | Labels | Meaning | Scope |
|--------|------|--------|---------|-------|
| `medcore_notification_worker_ticks_total` | Counter | `result` ∈ {`success`,`error`} | One increment per Tick return | **PROCESS-LOCAL** |
| `medcore_notification_worker_tick_duration_seconds` | Histogram | *(none)* | Wall-clock Tick duration (recover + claim + process + snapshot refresh) | **PROCESS-LOCAL** |
| `medcore_notification_worker_claimed_total` | Counter | *(none)* | Intents successfully claimed into `PROCESSING` | **PROCESS-LOCAL** |
| `medcore_notification_worker_stale_recovered_total` | Counter | *(none)* | Stale `PROCESSING` intents successfully recovered | **PROCESS-LOCAL** |
| `medcore_notification_delivery_attempts_total` | Counter | `channel`, `outcome` | One increment per claimed-intent handling branch | **PROCESS-LOCAL** |
| `medcore_notification_provider_duration_seconds` | Histogram | `channel`, `provider` | Wall time of provider `Send` only | **PROCESS-LOCAL** |
| `medcore_notification_queue_pending` | Gauge | `channel` | Count of `PENDING` intents | **GLOBAL DB SNAPSHOT** |
| `medcore_notification_queue_due` | Gauge | `channel` | `PENDING` with `send_after <= asOf` | **GLOBAL DB SNAPSHOT** |
| `medcore_notification_queue_processing` | Gauge | `channel` | Count of `PROCESSING` intents | **GLOBAL DB SNAPSHOT** |
| `medcore_notification_queue_stale_processing` | Gauge | `channel` | Stale `PROCESSING` (lease past cutoff) | **GLOBAL DB SNAPSHOT** |
| `medcore_notification_queue_oldest_due_age_seconds` | Gauge | `channel` | Age of oldest due `PENDING` (`asOf - min(send_after)`) | **GLOBAL DB SNAPSHOT** |

### Bounded label values

**Delivery / provider (5C)**

- `channel`: `log`, `email`
- `outcome`: `sent`, `skipped`, `permanent`, `invalid_message`, `not_configured`, `ambiguous`, `transient`, `canceled`, `invalid_payload`, `adapter_unavailable`
- `provider` pairs: (`log`,`log`), (`email`,`microsoft365`)

**Queue gauges (5D)**

- `channel`: `log`, `email`, `sms`

There is **no** `other` / `unknown` / `failed` metric label. Unknown domains are dropped at observation time.

**SMS note:** queue gauges may emit `channel="sms"`. SMS is normally zero today (no delivery adapter). A non-zero SMS backlog can reveal orphaned persisted work.

---

## 2. Scrape contract

| Item | Value |
|------|--------|
| Endpoint | `GET /metrics` |
| Same listener as | `GET /healthz`, `GET /readyz` |
| Bind | `0.0.0.0:<NOTIFICATION_WORKER_HEALTH_PORT>` |
| Default port | `8081` |
| Registry | Private process registry (not Prometheus default) |
| Collectors | Application metrics only (no Go/process collectors) |
| Auth | **Unauthenticated** |

**Scrape behavior:** in-memory gather only.

- `/metrics` does **not** query PostgreSQL
- `/metrics` does **not** call Microsoft Graph
- `/metrics` does **not** refresh queue snapshots (Tick path only)

### Scrape security

Production contract:

- Expose the health/metrics listener on a **private / internal** network only
- Allow scrape from Prometheus (or equivalent) on that network
- Do **not** expose `/healthz`, `/readyz`, or `/metrics` on the public internet
- Enforce access with the platform’s usual controls (e.g. NetworkPolicy, security group / firewall, private service network, internal reverse proxy)

Application-level metrics auth is **not** part of 5F. Broader production M365/auth posture is **26I-6**.

### Recommended Prometheus job label

Examples in this document use:

```text
job="medcore-notification-worker"
```

This is a **documentation convention**. Actual scrape config must use the same
`job` label or adapt every PromQL example accordingly. This repository does
not ship `prometheus.yml`.

---

## 3. Multi-replica aggregation — CRITICAL

### PROCESS-LOCAL (5B / 5C)

Counters and histograms are emitted independently by each worker replica.

Across replicas, use:

- `sum(...)`
- `rate(...)`
- `sum by (...) (rate(...))`

as appropriate. Aggregate histogram **buckets** across replicas **before**
`histogram_quantile`.

### GLOBAL DB SNAPSHOT (5D)

All five queue gauges are cached snapshots of the **same PostgreSQL queue**,
refreshed once per Tick on each replica.

**NEVER** sum queue gauges across replicas:

```promql
# WRONG — multiplies the same logical backlog by replica count
sum(medcore_notification_queue_due)
```

**Preferred:**

```promql
max by (channel) (medcore_notification_queue_pending)
max by (channel) (medcore_notification_queue_due)
max by (channel) (medcore_notification_queue_processing)
max by (channel) (medcore_notification_queue_stale_processing)
max by (channel) (medcore_notification_queue_oldest_due_age_seconds)
```

`max` is the conservative operational view under slight snapshot-time skew.
Alternative: scrape / select **one** designated worker target for global gauges.

> **Dashboard warning**
>
> | Metric class | Aggregation |
> |--------------|-------------|
> | PROCESS-LOCAL (ticks, claimed, delivery, provider histograms) | `sum` / `rate` / bucket sum |
> | GLOBAL DB GAUGES (queue_*) | `max by (channel)` — **NEVER `sum`** |

---

## 4. Recommended dashboard: Notification Worker

One dashboard, five sections.

### 4.1 Worker health

**Target up**

```promql
up{job="medcore-notification-worker"}
```

Application counters alone are **not** a liveness guarantee. Prefer Prometheus
`up` plus `/healthz` / `/readyz`.

**Tick rate**

```promql
sum(rate(medcore_notification_worker_ticks_total[5m]))
```

**Tick error rate**

```promql
sum(rate(medcore_notification_worker_ticks_total{result="error"}[5m]))
```

**Tick error ratio**

```promql
sum(rate(medcore_notification_worker_ticks_total{result="error"}[5m]))
/
clamp_min(sum(rate(medcore_notification_worker_ticks_total[5m])), 1e-9)
```

**Claim rate**

```promql
sum(rate(medcore_notification_worker_claimed_total[5m]))
```

**Stale recovered rate**

```promql
sum(rate(medcore_notification_worker_stale_recovered_total[5m]))
```

**Tick duration p50 / p95 / p99**

```promql
histogram_quantile(
  0.50,
  sum by (le) (rate(medcore_notification_worker_tick_duration_seconds_bucket[5m]))
)

histogram_quantile(
  0.95,
  sum by (le) (rate(medcore_notification_worker_tick_duration_seconds_bucket[5m]))
)

histogram_quantile(
  0.99,
  sum by (le) (rate(medcore_notification_worker_tick_duration_seconds_bucket[5m]))
)
```

Aggregate `_bucket` series across replicas **before** `histogram_quantile`.
Never average per-replica quantiles.

### 4.2 Queue / backlog

> GLOBAL DB GAUGES — use `max by (channel)`, never `sum`.

```promql
max by (channel) (medcore_notification_queue_pending)
max by (channel) (medcore_notification_queue_due)
max by (channel) (medcore_notification_queue_processing)
max by (channel) (medcore_notification_queue_stale_processing)
max by (channel) (medcore_notification_queue_oldest_due_age_seconds)
```

### 4.3 Delivery outcomes

```promql
sum by (channel, outcome) (
  rate(medcore_notification_delivery_attempts_total[5m])
)
```

Outcomes: `sent`, `skipped`, `permanent`, `invalid_message`, `not_configured`,
`ambiguous`, `transient`, `canceled`, `invalid_payload`, `adapter_unavailable`.

**Do not** treat `skipped` or `canceled` as availability failures by themselves.

Optional operational sent ratio (explanatory denominator required; **NOT AN SLA**):

```promql
sum(rate(medcore_notification_delivery_attempts_total{outcome="sent"}[5m]))
/
clamp_min(sum(rate(medcore_notification_delivery_attempts_total[5m])), 1e-9)
```

This mixes legitimate `skipped` / `canceled` into the denominator — interpret carefully.

### 4.4 Provider latency

Expected production combinations:

- `channel="log"`, `provider="log"`
- `channel="email"`, `provider="microsoft365"`

**p50 / p95 / p99**

```promql
histogram_quantile(
  0.50,
  sum by (le, channel, provider) (
    rate(medcore_notification_provider_duration_seconds_bucket[5m])
  )
)

histogram_quantile(
  0.95,
  sum by (le, channel, provider) (
    rate(medcore_notification_provider_duration_seconds_bucket[5m])
  )
)

histogram_quantile(
  0.99,
  sum by (le, channel, provider) (
    rate(medcore_notification_provider_duration_seconds_bucket[5m])
  )
)
```

No HTTP status label exists on this metric.

### 4.5 Reliability / recovery

Combine: stale recovered rate, queue stale gauge, tick errors, ambiguous /
transient delivery rates. Link operators to the alert table and ambiguous
runbook below.

---

## 5. Alert threshold disclaimer

**The thresholds below are INITIAL OPERATIONAL DEFAULTS.**

They are **NOT** contractual SLAs. Tune after observing production traffic,
queue volume, provider latency, and replica topology.

Code anchors (verify in source if changing):

| Constant | Value |
|----------|--------|
| Worker poll default | `NotificationWorkerPollDefault` = **2s** |
| Stale processing lease | `NotificationStaleProcessing` = **15m** |
| Max attempts | `NotificationMaxAttempts` = **5** |
| Retry backoff after failed attempt 1..4 | **1m**, **5m**, **15m**, **1h**; attempt 5 terminal |

---

## 6. Recommended alerts

Severity model: **warning** | **critical**.

PromQL assumes `job="medcore-notification-worker"` where `up` is used.

### NotificationWorkerDown

| Field | Value |
|-------|--------|
| Severity | critical |
| PromQL | `up{job="medcore-notification-worker"} == 0` |
| Initial `for` | `1m` |
| Meaning | Prometheus cannot scrape the worker target |
| First checks | deployment/container/process; `/healthz`; `/readyz`; DB reachability; worker configuration |

### NotificationWorkerTickErrors

| Field | Value |
|-------|--------|
| Severity | warning |
| PromQL | see below |
| Initial `for` | `10m` |
| Meaning | Sustained Tick error ratio (>10% of ticks) |
| First checks | DB connectivity; claim/recover path; application logs `operation=tick` + bounded `error_class` |

```promql
sum(rate(medcore_notification_worker_ticks_total{result="error"}[5m]))
/
clamp_min(sum(rate(medcore_notification_worker_ticks_total[5m])), 1e-9)
> 0.10
```

Tune threshold and window for low-traffic sites (ratio spikes on sparse ticks).

### NotificationQueueOldestDue

| Field | Value |
|-------|--------|
| Severity | warning |
| PromQL | `max by (channel) (medcore_notification_queue_oldest_due_age_seconds) > 300` |
| Initial `for` | `10m` |
| Meaning | Oldest due work age > 5 minutes (operational default) |
| First checks | worker `up`; tick errors; due backlog; delivery outcomes; provider latency; DB health |

### NotificationQueueOldestDueCritical

| Field | Value |
|-------|--------|
| Severity | critical |
| PromQL | `max by (channel) (medcore_notification_queue_oldest_due_age_seconds) > 1800` |
| Initial `for` | `15m` |
| Meaning | Oldest due work age > 30 minutes (operational default) |
| First checks | same as warning; escalate drain vs EMAIL/LOG failure modes |

5m / 30m are starting points, **not** SLAs.

### NotificationQueueStaleProcessing

| Field | Value |
|-------|--------|
| Severity | warning |
| PromQL | `max by (channel) (medcore_notification_queue_stale_processing) > 0` |
| Initial `for` | `20m` |
| Meaning | Stale `PROCESSING` persists after many Ticks (canonical lease = 15m) |
| First checks | stale recovery; tick health; DB locks/connectivity |

### NotificationDeliveryTransientHigh

| Field | Value |
|-------|--------|
| Severity | warning |
| PromQL | see below |
| Initial `for` | `15m` |
| Meaning | Sustained high share of `transient` outcomes |
| First checks | Graph/network; provider p95; Retry-After floors; external connectivity |

```promql
sum(rate(medcore_notification_delivery_attempts_total{outcome="transient"}[10m]))
/
clamp_min(sum(rate(medcore_notification_delivery_attempts_total[10m])), 1e-9)
> 0.25
```

**Low traffic:** prefer an absolute event/rate floor at the site (e.g. require
minimum attempt rate before evaluating the ratio), or use a pure absolute
`transient` rate rule instead.

### NotificationDeliveryNotConfigured

| Field | Value |
|-------|--------|
| Severity | critical |
| PromQL | `sum(rate(medcore_notification_delivery_attempts_total{outcome="not_configured"}[5m])) > 0` |
| Initial `for` | `5m` |
| Meaning | Delivery attempts hit `not_configured` (EMAIL path misconfiguration when work is claimed) |
| First checks | EMAIL enablement; required M365 env var **presence** only; worker config — **never print secrets** |

### NotificationDeliveryAdapterUnavailable

| Field | Value |
|-------|--------|
| Severity | critical |
| PromQL | `sum(rate(medcore_notification_delivery_attempts_total{outcome="adapter_unavailable"}[5m])) > 0` |
| Initial `for` | `5m` |
| Meaning | Claimed channel has no registered adapter |
| First checks | configured channels; adapter registration; EMAIL enablement; worker startup |

### NotificationDeliveryInvalidPayload

| Field | Value |
|-------|--------|
| Severity | warning |
| PromQL | `sum(rate(medcore_notification_delivery_attempts_total{outcome="invalid_payload"}[5m])) > 0` |
| Initial `for` | `10m` |
| Meaning | Persisted intent payload cannot be parsed/processed |
| First checks | enqueue/lifecycle defects — **no payload content in alert annotations** |

### NotificationDeliveryInvalidMessage

| Field | Value |
|-------|--------|
| Severity | warning |
| PromQL | `sum(rate(medcore_notification_delivery_attempts_total{outcome="invalid_message"}[5m])) > 0` |
| Initial `for` | `10m` |
| Meaning | Message validation/render failure |
| First checks | renderer; validation; template/config — **no subject/body in annotations** |

### NotificationDeliveryAmbiguous

| Field | Value |
|-------|--------|
| Severity | warning |
| PromQL | `sum(rate(medcore_notification_delivery_attempts_total{outcome="ambiguous"}[15m])) > 0` |
| Initial `for` | `5m` |
| Meaning | Worker classified delivery outcome as ambiguous (possible provider accept) |
| First checks | follow **Ambiguous delivery runbook** below — do **not** blind replay |

### Permanent failures (dashboard-first)

`outcome="permanent"` is primarily a **dashboard** signal (provider policy /
unusable destination / provider rejection). If alerting:

- severity **warning** only
- require **sustained / elevated** rate (site-tuned), not a single event
- never put destination addresses in annotations

### Do **not** alert merely because

- `outcome="skipped"` — legitimate workflow skip
- `outcome="canceled"` — context cancel leaving PROCESSING (not availability)

---

## 7. Ambiguous delivery runbook

1. Confirm metric `outcome="ambiguous"` (distinct from `transient`).
2. Inspect durable notification intent + attempt history via the approved
   admin/read API (PHI-safe response contract).
3. Treat the message as **possibly accepted** by the provider.
4. **Do not** blindly replay / re-send.
5. If provider-side evidence is available, inspect it with approved operational
   tooling **without** copying recipient, content, or secrets into logs/tickets.
6. Determine external delivery state before any manual replay.
7. Escalate unresolved cases as delivery-integrity incidents — manual replay
   can create duplicates.
8. Distinguish from a claim that is merely `PROCESSING` / stale (recovery path).
9. Preserve the known **26H residual window**: external accept may occur before
   durable finalization; there is no automated “safe replay” in-product.

There is no automated recovery that closes this window in 5F.

---

## 8. Health / readiness / metrics

| Endpoint | Role |
|----------|------|
| `GET /healthz` | Process / run-loop **liveness** (started, not shutting down, Run still active). Does **not** require DB or Graph. |
| `GET /readyz` | Worker live **and** bounded `sql.DB.PingContext` (~1s). Does **not** require Graph/token success. |
| `GET /metrics` | In-memory Prometheus exposition (no DB / Graph). |

Microsoft Graph availability is **not** part of readiness. Provider issues
surface via delivery/provider metrics and (when abnormal) bounded worker logs
(`operation`, `error_class`).

---

## 9. Log correlation (LOT 26I-5E)

Operational application logs use structured `slog` with bounded fields such as:

- `operation` (e.g. `tick`, `queue_snapshot_refresh`, `finalize_sent`)
- `error_class` (bounded taxonomy)

Routine per-notification outcomes belong to **metrics** + durable attempt
history — not INFO application logs.

Operators should **not** expect patient/appointment/intent IDs in application logs.

---

## 10. Privacy / cardinality

Forbidden in metrics, dashboards, alert annotations, and Grafana variables:

- patient ID, appointment ID, intent ID, attempt ID
- recipient / email
- subject / body / notification payload
- idempotency key, request / correlation ID
- tenant / client ID
- DSN, secrets, tokens

Allowed dimensions remain bounded enums (`channel`, `outcome`, `provider`,
`result`).

---

## 11. Non-claims / preserved debt

- External Prometheus / Grafana / Alertmanager deployment is **not** proven by this doc
- Alert rules and dashboards are **not** deployed from this repository
- Thresholds are initial tuning defaults, **not** SLAs
- Known residual: 26H ambiguous send/finalize window (see runbook)
- Separate debt: GORM expected-miss SQL logging; `database.Connect` stdlib logging; durable attempt diagnostic text
- Production M365 security/runbook hardening is **26I-6**, not 5F
