# Medical Scheduling — LOT 23

## Canonical identity

**`patient_queue_appointments` is the canonical appointment table (Option A).**

There is no second appointments table. The same row ID flows through:

```
patient_queue_appointments
        ↓ CheckInAppointment
finance verification
        ↓
patient_queue_tickets
        ↓ WAITING_TRIAGE → … → Doctor Worklist → Consultation → LOT 22 completion
```

Walk-in tickets keep `appointment_id = NULL`.

## Concept separation

| Concept | Role | Status |
|---------|------|--------|
| **Appointment** | Booked interval for a patient | 23A |
| **Working Schedule** | Recurring staff hours (wall-clock) | **23B** |
| **Schedule Exception** | Date-specific absences / extra openings | **23B** |
| **Availability** | Derived free intervals | **23C** |
| **Generated Slot** | Ephemeral search result | **23C** (in memory only) |
| **Queue Ticket** | Operational clinical journey | Unchanged (LOT 19–22) |
| **Doctor Worklist** | Post-triage operational list | Unchanged |

Working Schedule ≠ Schedule Exception ≠ Availability ≠ Slot ≠ Appointment ≠ Queue Ticket ≠ Doctor Worklist.

---

## LOT 23B — Working schedules & exceptions

### Recurring schedule model

Table: `patient_queue_staff_working_schedules`

One row = one recurring weekly window (not one row per calendar date).

| Field | Meaning |
|-------|---------|
| `practitioner_id` | `users.id` (canonical practitioner) |
| `service_id` | `organization_services.id` |
| `weekday` | Go `time.Weekday` integer **0=Sunday … 6=Saturday** |
| `start_time` / `end_time` | PostgreSQL `TIME`, local wall-clock `HH:MM:SS` |
| `valid_from` / `valid_until` | Inclusive calendar dates; `valid_until` NULL = open-ended |
| `active` | Soft disable (DELETE API sets `active=false`) |

Example: practitioner=42, service=Cardiology, weekday=1 (Monday), 08:00–12:00, valid_from=2026-09-01, valid_until=NULL → every Monday 08:00–12:00 from that date onward **in the configured business timezone**.

### Weekday representation

**Canonical:** integer matching Go `time.Weekday` (0–6).

Do not mix `"MONDAY"`, `"Monday"`, `1`, `"1"` across layers. API and persistence use the integer only.

### Local wall-clock vs appointment timestamps

| | Working schedule | Appointment / Exception |
|--|------------------|-------------------------|
| Meaning | Recurring local clock hours | Concrete instants |
| Storage | `TIME` (`08:00:00`) | `timestamptz` / `time.Time` UTC |
| Fake epoch dates | **Forbidden** | N/A |

Appointments remain authoritative booked intervals. Changing a schedule **never** deletes/reschedules appointments (23B).

### Timezone configuration

- Env: **`MEDCORE_TIMEZONE`** (IANA name) — **scheduling wall-clock / recurrence only**
- Config field: `config.Timezone`
- Runtime: `internal/core/scheduling` (`Location()`)
- **Default:** `UTC` (consistent with current appointment UTC storage until a deployment sets an explicit zone)

Hospital civil calendar / SQL `CURRENT_DATE` alignment uses a **separate** env: **`MEDCORE_BUSINESS_TIMEZONE`**. See [`TEMPORAL_CONTRACT.md`](./TEMPORAL_CONTRACT.md). Do not silently treat `MEDCORE_TIMEZONE` as the global business calendar.

LOT 23C will convert `Monday 08:00` + date → real timestamps via `time.Date(..., scheduling.Location())`. **DST uses IANA rules**, not fixed UTC offsets. Do not scatter hard-coded `Europe/Paris` in services.

### Validity periods

Inclusive date range. Two windows for the same practitioner/service/weekday may share clock times only if their validity date ranges do **not** overlap.

### Multiple daily windows

Independent rows. Valid:

```
Monday 08:00–12:00
Monday 14:00–18:00
```

Not modeled as morning/afternoon pair columns.

### Interval / overlap policy (schedules)

Half-open **`[start, end)`** on wall-clock time.

- Adjacent `08:00–12:00` and `12:00–16:00` → **allowed**
- Overlap `08:00–12:00` and `10:00–14:00` → **HTTP 409 Conflict**

Different services for the same practitioner may overlap in clock time (service-scoped capacity).

### Service scoping & practitioner validation

- Org model: Department → Service (`organization_services.id`). **No Facility/Site** in 23B.
- Before create/update: active `staff_profiles` for `users.id` + active `staff_service_assignments` for the target service.
- Frontend-supplied IDs are never trusted without this backend check.

### Schedule exceptions

Table: `patient_queue_schedule_exceptions`

Concrete `start_at` / `end_at` (`timestamptz`, exclusive end). Not recurring.

**Types:**

| Type | Polarity |
|------|----------|
| `ABSENCE`, `LEAVE`, `MEETING`, `BLOCKED`, `TRAINING`, `OTHER` | Negative (remove capacity) |
| `EXTRA_AVAILABILITY` | Positive (add capacity) |

**Overlap policy (23B write path):**

- Same polarity overlap → **reject 409**
- Positive ∩ negative → **allowed** at write time

**Precedence for LOT 23C:** **negative wins over positive** (`ExceptionPrecedenceNegativeWins()`).

Soft cancel: `active=false` + `cancelled_at` set.

### Concurrency (schedule definition)

Within a transaction:

1. `pg_advisory_xact_lock(practitioner_id, service_id<<8|weekday)`
2. Re-check overlap among active windows
3. Insert/update

This protects schedule-definition integrity under concurrent admin writes. **Not** appointment booking overlap (23D).

### Audit

Separate append-only table: `patient_queue_schedule_audit` (not appointment history).

Events: `SCHEDULE_CREATED`, `SCHEDULE_UPDATED`, `SCHEDULE_DISABLED`, `EXCEPTION_CREATED`, `EXCEPTION_UPDATED`, `EXCEPTION_CANCELLED`.

`actor_user_id` from JWT / `Access.UserID` only.

### RBAC (LOT 21 catalog + LOT 23I)

| Permission | Intent |
|------------|--------|
| `schedule.read.own` | Own schedules (JWT user) — **read only** |
| `schedule.read.service` | Assigned services — **read only** |
| `schedule.read.all` | Global read — **must not enlarge manage.service** |
| `schedule.manage.own` | Manage own (must still be assigned to service); **not** granted by default packs (product decision deferred) |
| `schedule.manage.service` | Manage **only** assigned services |
| `schedule.manage.all` | Global manage |

Staff packs (minimum): physicians → `schedule.read.own`; ACCUEIL → `schedule.read.service`; DIRECTEUR_MEDICAL → read.all + manage.service; DIRECTEUR_ADMINISTRATIF → read.all + manage.all.

**LOT 23I:** `schedule.read.all` never bypasses mutation scope. Helpers split read-scope vs manage-scope (`assertScheduleServiceInReadScope` / `assertScheduleServiceInManageScope`).

Enforcement on LIST/GET/CREATE/UPDATE/DELETE; load → authorize persisted scope → mutate. Retargeting `serviceId` requires source **and** target authorization.

### API

```
GET/POST          /api/schedules
GET               /api/schedules/mine   ← identity from JWT only
GET/PATCH/DELETE  /api/schedules/:id    ← DELETE = soft disable

GET/POST          /api/schedule-exceptions
GET/PATCH/DELETE  /api/schedule-exceptions/:id
```

Filters: `practitionerId`, `serviceId`, `weekday`, `active`, `date` / `from`+`to`.

### LOT 23B domain contract (inputs for 23C)

- `ListApplicableWorkingWindows(practitionerID, serviceID, fromDate, toDate)`
- `ListApplicableExceptions(practitionerID, serviceID, from, to)`
- `ExceptionPrecedenceNegativeWins()` → true

### Boundary with Queue / Worklist

After patient arrival, Queue / check-in / finance / triage / Doctor Worklist / LOT 22 remain authoritative. Schedules do not feed the worklist.

---

## LOT 23C — Medical availability engine

### Equation (derived, never persisted)

```
Recurring Working Windows
    ∪ Positive Exceptions (EXTRA_AVAILABILITY)
    − Negative Exceptions (negative wins)
    − Blocking Appointments
  = Free Intervals
    → Generated Candidate Slots (in memory)
```

**Availability response ≠ booking guarantee.** Between `GET /availability` and a future booking, another transaction may consume the interval. LOT **23D** re-checks under authoritative locks. No long-lived DB locks on availability reads.

### No slot table

Slots are ephemeral. **No** `availability_slots` / `generated_slots` migration.

### Interval algebra

Package `internal/core/scheduling`: half-open `[start,end)`.

Operations: Normalize, Merge (overlap + adjacent), Intersect, Subtract, Clip, GenerateSlots.

### Working schedule projection

Wall-clock `TIME` + calendar date → concrete instant via `scheduling.ProjectWallClock` using `MEDCORE_TIMEZONE` IANA location.

DST (Go `time.Date`):

- Spring gap (nonexistent): normalized forward
- Autumn fold (ambiguous): earlier occurrence
- Never fixed `UTC+1` / `UTC+2`

### Exception processing

1. Project active schedules for weekday + validity
2. Union `EXTRA_AVAILABILITY` into base (merge overlaps)
3. Subtract all negative exceptions (ABSENCE, LEAVE, MEETING, BLOCKED, TRAINING, OTHER)
4. Subtract blocking appointments (clipped to query)

### Appointment blocking matrix

| Status | Blocks? |
|--------|---------|
| `SCHEDULED` | yes |
| `ARRIVED` | yes |
| `CHECKED_IN` | yes |
| `IN_PROGRESS` | yes |
| `COMPLETED` | yes (deterministic occupancy) |
| `CANCELLED` | **no** |
| `NO_SHOW` | **no** (capacity released) |
| unknown | yes (fail closed) |

### Legacy `scheduled_end_at == NULL`

Read-time only (no DB mutation):

1. use `scheduled_end_at` when set
2. else type `default_duration_minutes` when type present
3. else `MEDCORE_LEGACY_APPOINTMENT_FALLBACK_MINUTES` (default **30**)

### Duration resolution

- `appointmentTypeId` only → type duration
- `durationMinutes` only → explicit
- both → **reject** if inconsistent
- neither → reject
- type with `service_id` set must match query `serviceId`

Limits: duration 5–480 min; step 5–240 min (default step = duration); range ≤ **31** days; max **10000** slots (reject if exceeded).

### Slot generation

Align from the **start of each free interval** (not Unix epoch). No partial slots. `step` may be `<` duration.

### Service-wide availability

Eligible practitioners = active `staff_profiles` + active `staff_service_assignments` for the service (no parallel doctor table; no reliable exclusive clinician flag beyond assignment — capacity further constrained by schedules/exceptions).

Slots keep `practitionerId`. Sort: `startAt ASC`, `practitionerId ASC`, `endAt ASC`.

**Query strategy (no N+1):** one eligible-practitioner query + one schedules batch + one exceptions batch + one appointments batch + optional appointment-types batch; group in memory.

### First available

`GET /api/availability/first` — earliest candidate, **read-only** (no hold/lock/create). 404 when empty. Optional `to` (default +7 days from `from`).

### Own availability

`GET /api/availability/mine` — practitioner from JWT; `serviceId` still required among assigned services.

### API

```
GET /api/availability
GET /api/availability/first
GET /api/availability/mine
```

Query: `serviceId`, `practitionerId?`, `appointmentTypeId?`, `durationMinutes?`, `from`, `to`, `slotStepMinutes?` (RFC3339).

### RBAC

Reuses `schedule.read.own|service|all` (and manage.* for route convenience). No separate `availability.read`. Service scope enforced; own-only cannot enumerate service-wide practitioners.

### Domain API for LOT 23D

- `ComputeAvailability(query, access)`
- `FirstAvailable(query, access)`
- `IsIntervalAvailable(practitionerID, serviceID, start, end, access)` — **not** concurrency protection

### Mutation safety

Availability is read-only: never creates/updates appointments, schedules, exceptions, or queue tickets.

---

## LOT 23D — Transactional booking & double-booking protection

### Snapshot vs booking

`GET /availability` is a **snapshot**, not a reservation.

`POST /api/appointments` is the authoritative booking path. It always:

1. validates request + duration
2. resolves candidates (specific or auto)
3. `BEGIN`
4. advisory-locks patient, then practitioner(s) ascending
5. re-checks patient overlap
6. re-checks schedule ∪ EXTRA − negatives − appointments (full containment)
7. inserts appointment with non-null `scheduled_end_at`
8. appends history `CREATED` (actor = JWT)
9. `COMMIT` (or `ROLLBACK` on any failure)

### Lock strategy

PostgreSQL `pg_advisory_xact_lock` (same family as 23B schedule definition locks):

| Resource | key1 | key2 |
|----------|------|------|
| Idempotency (caller+key) | `230403` | `int32(FNV-1a32("{userID}:{key}"))` |
| Patient booking | `230401` | `patient_id` |
| Practitioner booking | `230402` | `practitioner_id` |

**Lock order (deadlock prevention):**
1. If idempotency key present: idempotency lock first
2. Patient
3. All candidate practitioners in **ascending ID** order

Then try candidates in that same ascending order.

Hash collisions on the idempotency lock key only serialize unrelated (caller,key) pairs briefly; uniqueness remains on `(created_by, idempotency_key)`.

No process-local mutexes. No long-lived locks outside the transaction.

### Overlap rules

Half-open `[start, end)`.

Overlap: `existing.start < requested.end AND existing.end > requested.start`.

Adjacent allowed. Blocking statuses = shared `AppointmentBlocksAvailability` (same matrix as 23C). Legacy NULL end uses `ResolveAppointmentEnd` (same fallback as 23C).

Patient overlap is independent of practitioner: one patient cannot hold two blocking intervals that overlap, even across practitioners/services.

### Automatic practitioner selection

Eligible staff assigned to the service whose free intervals fully contain the request (snapshot). Deterministic order: **practitioner ID ascending**. Under transaction, try each candidate; first that still fits wins. If all conflict → **409**.

### Duration

Same policy as 23C. New bookings **always** persist `scheduled_end_at` (never NULL).

### Idempotency

Optional `idempotencyKey` body field or `Idempotency-Key` header.

**Identity:** `(created_by, idempotency_key)` — caller-scoped. User A key `abc` does not block User B key `abc`.

**Partial unique index:** `ux_pq_appt_idempotency_caller ON (created_by, idempotency_key) WHERE idempotency_key IS NOT NULL`.

`cmd/migrate` (sole schema owner) drops obsolete global `ux_pq_appt_idempotency` if present, then creates the caller-scoped index.

**Same semantic booking request** (exact match required for reuse):

- caller (`created_by`)
- patient
- service
- requested start + resulting end (duration)
- appointment type identity (`appointment_type_id`, not duration alone)
- practitioner intent: specific practitioner must match; auto-assignment (`practitionerId` omitted) remains auto (stored winner may differ only if re-executed as create — reuse returns prior row as-is)
- **reason** — **included** in semantics (`strings.TrimSpace`); different reason with same key → **409**

Behavior:

- Same caller + key + same semantics → return original appointment (**200** if reused, **201** on first create)
- Same caller + key + different semantics → **409**
- Concurrent identical retries: advisory lock serializes; both succeed with the **same** appointment ID (never 409 for identical retry)
- `created_by` is always JWT actor (non-null) on new bookings

### RBAC (LOT 23D + 23I)

Canonical booking permissions:

`appointment.create.service` | `appointment.create.all` | `schedule.manage.service` | `schedule.manage.all` | `*`

**`queue.checkin` is NOT booking authority** (LOT 23I). It remains check-in / walk-in / finance only.

Service scope for booking uses **staff assignments** (`assignedStaffServiceIDs`).
`queue.read.all` and `schedule.read.all` **must not** expand `appointment.create.service` / `schedule.manage.service` to global.

- `appointment.create.all` / `schedule.manage.all` / `*` → global create
- `appointment.create.service` / `schedule.manage.service` → assigned services only

Practitioner must be assigned to service. Patient must exist.

Packs:

- **ACCUEIL** (+ legacy role `accueil`): `appointment.create.service` (+ `schedule.read.service`, `queue.checkin`, cancel/no_show.service)
- **DIRECTEUR_MEDICAL**: `appointment.create.service` (SERVICE mutations even with `schedule.read.all` / `queue.read.all`)
- **DIRECTEUR_ADMINISTRATIF**: `appointment.create.all`
- **Physician**: no automatic `appointment.create.*`

### API

```
POST /api/appointments          → BookAppointment (authoritative)
POST /api/queue/appointments    → CreateAppointment → delegates to BookAppointment
                                  (legacy/deprecated path; **same** booking RBAC as above — LOT 23I)
```

Both HTTP paths that **insert** scheduled appointments use the same transactional guarantees (locks, schedule, overlaps, non-null `scheduled_end_at`) and the **same** permission set.

Legacy body maps `expectedDoctorId` → `practitionerId`, `scheduledAt` → `startAt`. Requires `appointmentTypeId` and/or `scheduledEndAt` (no silent duration invent). Walk-in check-in is unchanged and does not create appointments via this path.

### Indexes / EXCLUDE

Existing `(expected_doctor_id|patient_id|service_id, scheduled_at)` indexes used.

Partial unique: `ux_pq_appt_idempotency_caller` on `(created_by, idempotency_key) WHERE NOT NULL`.

**No EXCLUDE constraint:** legacy NULL ends + status-based blocking make a safe EXCLUDE brittle; advisory locks are mandatory.

### Deferred (23E+)

Reschedule, cancel API changes, reminders, holds, waitlists, recurring series, frontend calendar/wizard.

---

## Appointment interval semantics (23A)

- `scheduled_at` = **start inclusive**
- `scheduled_end_at` = **end exclusive** → half-open `[start, end)`
- Invariant: `end > start` when end is set
- Adjacent intervals `09:00–09:30` and `09:30–10:00` do **not** overlap
- Legacy / queue-only creates may leave `scheduled_end_at` **NULL** (pre-23D rows only)
- HTTP CreateAppointment / BookAppointment always set `scheduled_end_at`
- When `appointment_type_id` is set and end omitted, end = start + `default_duration_minutes`

## AppointmentType

Table `patient_queue_appointment_types`: unique `code`, `default_duration_minutes` in `[MinDurationMinutes, MaxDurationMinutes]` (5–480), optional `service_id`, soft `active`.

### Administration (LOT 23M-A)

| Method | Path | Permission |
|--------|------|------------|
| `GET` | `/api/appointment-types` | `schedule.read.own` \| `.service` \| `.all` (unchanged) |
| `POST` | `/api/appointment-types` | `appointment_type.manage` \| `*` |
| `PATCH` | `/api/appointment-types/:id` | `appointment_type.manage` \| `*` |
| `DELETE` | `/api/appointment-types/:id` | soft-deactivate (`active=false`); same manage permission |

- **Not** authorized by `queue.checkin`, `schedule.read.*`, `schedule.manage.*`, or `appointment.create.*`.
- Pack grant: `DIRECTEUR_ADMINISTRATIF` (not `DIRECTEUR_MEDICAL`).
- `code` is immutable on update; duration bounds match booking limits.
- Optional `service_id` must reference an **active** `organization_services` row (`clearServiceId` clears it).
- Soft-deactivate only — no hard delete; historical appointments remain readable and enrichable.
- Audit: `patient_queue_schedule_audit` with `entityType=APPOINTMENT_TYPE`.

### Organization service activity (LOT 23M-A)

New scheduling **writes** and **availability** require `organization_services.active=true` (central `assertServiceExists`). Covers booking, reschedule, working schedules, schedule exceptions, and availability queries. Existing appointments for a later-deactivated service remain **readable**. Practitioner eligibility still requires active `staff_profiles` + active `staff_service_assignments`.

## Practitioner identity

`expected_doctor_id` / schedule `practitioner_id` → **`users.id`**.

## Lifecycle vs Queue

**Before check-in** — appointment owns scheduling status.

**After check-in** — Queue ticket owns operational flow (`WAITING_TRIAGE` → … → `COMPLETED`). Appointment status is synchronized by existing LOT 22 writers (`TakeDoctor` → `IN_PROGRESS`, `Complete` → `COMPLETED`).

**Models stay separate:** an appointment may exist without a ticket; a walk-in ticket may exist without an appointment. After successful check-in, bidirectional link: `appointment.queue_ticket_id` ↔ `ticket.appointment_id`.

---

## LOT 23F — Appointment check-in / reception / queue integration

### Canonical API

`POST /api/queue/appointments/:id/check-in` (`queue.checkin`) → `CheckInAppointment`.

Walk-in remains independent: `POST /api/queue/check-in/walk-in` — **no appointment required**.

### Transaction (atomic)

```
BEGIN
  SELECT appointment FOR UPDATE
  service scope (out-of-scope → 404)
  if already CHECKED_IN / linked → validateCompletedCheckInReuse (full link) then return existing (HTTP 200)
  validate lifecycle (SCHEDULED / ARRIVED only; terminals reject)
  early check-in timing policy
  advisory lock patient (230401)
  reject if patient has ACTIVE ticket
  EvaluateFinance (LOT 19) — PAYMENT_REQUIRED / BLOCKED without override → reject (appointment stays SCHEDULED)
  create ticket WAITING_TRIAGE (patient/service/expected doctor from appointment)
  link both sides; appointment → CHECKED_IN
  appointment history CHECKED_IN + queue history CHECK_IN
COMMIT
```

Lock order (compatible with 23E cancel / no-show / reschedule): **appointment `FOR UPDATE` → patient advisory**. No practitioner lock on check-in.

### Timing policy

`MEDCORE_APPOINTMENT_EARLY_CHECKIN_MINUTES` (default **60**).

Earliest check-in = `scheduled_at − N minutes`. Before that → 400. Late arrival while still `SCHEDULED` is allowed (no auto no-show). Uses absolute UTC instants; wall-clock ops should keep `MEDCORE_TIMEZONE` consistent with scheduling.

### Idempotency / uniqueness

Natural key: appointment↔ticket linkage. Retry returns the same ticket (HTTP **200** when reused; **201** on create) **only** when `validateAppointmentTicketLink` passes:

- `queue_ticket_id` ↔ ticket id
- `ticket.appointment_id` ↔ appointment id
- patient_id / service_id match
- scheduled `expected_doctor_id` match when appointment has one (ignores `doctor_taken_by` / TakeDoctor)

Partial unique index `ux_pq_tickets_appointment` on `patient_queue_tickets(appointment_id) WHERE appointment_id IS NOT NULL`.

Orphan / incomplete link (e.g. `SCHEDULED` + ticket.`appointment_id` set, no `queue_ticket_id`) or any mismatch → **409** Conflict — no soft success, no auto-repair.

`EnsureTicketIndexes` is a **hard** migrate invariant (`cmd/migrate` fatals). Duplicate historical `appointment_id` values fail clearly without silent repair. API startup does not create this index (LOT 26I-3).

Same gate as walk-in. Booking does **not** bypass finance. Failure creates no ticket and does not mark `CHECKED_IN`.

### RBAC

`queue.checkin` only for check-in (23E: not for reschedule / cancel / no-show). Cross-service → 404.

### Non-goals preserved

No triage bypass, no consultation at check-in, no schedule/exception/slot mutation, no fake appointments for walk-ins.

## Appointment history (23A)

Table `patient_queue_appointment_history` — separate from schedule audit. Booking uses event `CREATED`.

## Overlap protection (appointments)

**LOT 23D:** transactional advisory locks + overlap re-check. No persisted slots.

**LOT 23E:** reschedule/cancel/no-show under same locks + `FOR UPDATE`; self-exclusion on overlap.

## Indexes

**23A appointments:** service/doctor/patient/status/type + `scheduled_at`.

**23B schedules:** `(practitioner_id, service_id, weekday)`, validity/active; exceptions range; audit entity.

**23C:** no new tables; uses existing indexes for batched loads.

**23D:** caller-scoped partial unique idempotency index `(created_by, idempotency_key)`; no slot table; no EXCLUDE.

**23F:** partial unique `ux_pq_tickets_appointment` (one ticket per appointment; walk-in `appointment_id` NULL).

---

## LOT 23F.1 — Scheduling read APIs (agenda)

Minimal authoritative reads for medical agenda (no new models / slots).

### Routes

| Method | Path | Permission |
|--------|------|------------|
| `GET` | `/api/appointments` | `schedule.read.own` \| `service` \| `all` |
| `GET` | `/api/appointments/:id` | same |
| `GET` | `/api/appointment-types` | same |

`queue.checkin` and `consultations.read` do **not** grant agenda read.

### `GET /api/appointments`

**Required query:** `from`, `to` (RFC3339).

**Optional:** `serviceId`, `practitionerId` (filters `expected_doctor_id`), `patientId`, `status`, `appointmentTypeId`, `page`, `limit` (max 100).

**Range:** half-open intersection `[from, to)`:

- `scheduled_at < to`
- effective end `> from`

**Effective end** (matches availability `ResolveAppointmentEnd`):

1. `scheduled_end_at` when set;
2. else appointment-type `default_duration_minutes`;
3. else legacy **30** minutes (`MEDCORE_LEGACY_APPOINTMENT_FALLBACK_MINUTES` / `LegacyAppointmentFallbackMinutes`).

**Max range:** `scheduling.MaxQueryRangeDays` (**31**).

**Sort:** `scheduled_at ASC, id ASC`.

**Scope:**

- `schedule.read.all` / `*`: all services;
- `schedule.read.service`: assigned services (SQL `IN`);
- `schedule.read.own`: `expected_doctor_id = JWT user` (not `created_by` / `doctor_taken_by`);
- OWN+SERVICE: union.

Out-of-scope service filter / GET → **404**. OWN requesting another `practitionerId` → **403**.

**Response:** `{ items: AppointmentDTO[], total, page, limit }` — same enrichment as today list + `durationMinutes`. Batched enrichment (no N+1).

### `GET /api/appointments/:id`

Same DTO + scope. Missing/out-of-scope → **404**.

### `GET /api/appointment-types`

Query: `serviceId?` (includes global `service_id NULL` + matching service), `active?` (`true`/`false`; omitted = all).

Response: `{ items: AppointmentType[] }`. Inactive types remain available for historical appointment enrichment.

### Today endpoint

`GET /api/queue/appointments/today` unchanged (permission `queue.reception.read`, same envelope). Internals reuse batched `enrichAppointments`.

## Future phases / status

| Phase | Focus | Status |
|-------|--------|--------|
| 23G | Reception / practitioner calendars (frontend) | **Delivered** |
| 23H | Patient 360 upcoming RDV | **Delivered** |
| 23I | RBAC hardening Scheduling / Appointments | **Delivered** |
| 23J | QA / release gate Scheduling | **Delivered** |
| 23K | Patient 360 history + Agenda patient deep-link (frontend) | **Delivered** |
| 23L | Schedule Administration (frontend) | **Delivered** |
| 23M-A | Inactive org-service hardening + Appointment Type admin API | **This lot** |

---

## LOT 23E — Appointment lifecycle (reschedule / cancel / no-show)

### State machine

| From \ Op | Reschedule | Cancel | No-show |
|-----------|------------|--------|---------|
| SCHEDULED | yes | yes | yes if `scheduled_at ≤ now` |
| ARRIVED | no | no | yes if time eligible & no ticket |
| CHECKED_IN | no | no | no |
| IN_PROGRESS | no | no | no |
| COMPLETED | terminal | terminal | terminal |
| CANCELLED | terminal | idempotent OK | terminal |
| NO_SHOW | terminal | terminal | idempotent OK |

Operational rule: if `queue_ticket_id` is set or status is CHECKED_IN / IN_PROGRESS → **reject** (LOT 23E does **not** mutate queue tickets).

### Reschedule semantics

- **Same** `patient_queue_appointments.id` (never cancel+recreate).
- `service_id` immutable.
- Omitted `practitionerId` = **keep current** practitioner (not auto-assign).
- Duration: keep current unless type/duration explicitly changed (23C/23D rules).
- Always non-null `scheduled_end_at`.
- Self-exclusion: overlap / availability checks exclude the appointment being moved.
- **Required concurrency precondition:** `expectedScheduledAt` + `expectedScheduledEndAt` must match the row under `FOR UPDATE`. Mismatch → **409** (stale). Concurrent writers from the same expected state: exactly one succeeds; the other gets 409. Last-writer-wins across different expected bases is removed for same-origin races.

### Transaction / locks

```
BEGIN
  [lifecycle idempotency advisory lock if key]
  SELECT appointment FOR UPDATE
  validate scope + state + no queue link
  validate expectedScheduledAt/EndAt (stale → 409)
  resolve interval
  lock patient → practitioners (old∪new, ascending, dedup)
  re-read + re-check expected precondition
  patient overlap excluding self
  practitioner availability excluding self
  UPDATE appointment
  append RESCHEDULED history (payload old/new JSON)
COMMIT
```

Lock namespaces (reuse 23D + lifecycle):

| Resource | key1 | key2 |
|----------|------|------|
| Lifecycle idempotency | `230404` | `int32(FNV(op:apptID:caller:key))` |
| Patient | `230401` | `patient_id` |
| Practitioner | `230402` | `practitioner_id` |

Concurrent reschedules sharing the **same expected** interval: one **200**, one **409 stale**. A client that re-reads the new interval may reschedule again successfully.

### Cancellation

`POST /api/appointments/:id/cancel` → status `CANCELLED`, row preserved, original booking reason untouched, cancel reason in history. Immediately non-blocking (23C matrix).

**Terminal idempotence:** already `CANCELLED` + cancel again (with or without key that does not conflict) → **200 no-op**, no new history row. Does not reopen.

### No-show

`POST /api/appointments/:id/no-show` (and legacy `/api/queue/appointments/:id/no-show`).

Rule: `scheduled_at ≤ now` (UTC compare). Future appointments → **400**.

**Terminal idempotence:** already `NO_SHOW` + no-show again → **200 no-op**, no duplicate history.

### History

Append-only `patient_queue_appointment_history`:

- `RESCHEDULED` — payload `{old,new,idempotencyKey?}` with practitioner/start/end/type
- `CANCELLED` / `NO_SHOW` — reason + optional idempotency key in payload

Actor = JWT `Access.UserID` only.

### Idempotency (lifecycle)

Optional `idempotencyKey` / `Idempotency-Key` header. Scoped as caller + operation + appointment + key.

Lookup: load recent history rows by `(appointment_id, actor_user_id, event_type)`, **JSON-unmarshal** `payload` (TEXT), compare `IdempotencyKey` with **exact string equality** (not substring). Malformed payloads skipped. `abc` ≠ `abc2`.

Semantic equality:

- **Reschedule:** start, end, type ID, practitioner, normalized reason (+ key/caller/op/appt)
- **Cancel / No-show:** normalized reason (+ key/caller/op/appt)

Same scoped key + same semantics → reuse without duplicate history. Same key + different semantics → **409**. Booking idempotency keys are **not** reused.

### RBAC

| Op | Permissions |
|----|-------------|
| Reschedule | `appointment.reschedule.service` \| `appointment.reschedule.all` \| `schedule.manage.service` \| `schedule.manage.all` \| `*` |
| Cancel | `appointment.cancel.service` \| `appointment.cancel.all` \| `*` |
| No-show | `appointment.no_show.service` \| `appointment.no_show.all` \| `*` |

**`queue.checkin` is NOT lifecycle authority** (check-in only — not booking, not reschedule/cancel/no-show).

Function grants:

- **ACCUEIL:** `appointment.create.service`, `appointment.cancel.service`, `appointment.no_show.service` (no reschedule)
- **DIRECTEUR_MEDICAL:** `appointment.create.service`, `appointment.reschedule.service`, `appointment.cancel.service`, `appointment.no_show.service` (+ `schedule.manage.service`)
- **DIRECTEUR_ADMINISTRATIF:** `appointment.create.all`, `appointment.reschedule.all`, `appointment.cancel.all`, `appointment.no_show.all` (+ `schedule.manage.all`)

Service scope via **staff assignments** (not Queue `assertCanAccessService`).
`.all` listed for the op / `schedule.manage.all` (reschedule) / `*` bypass.
`queue.read.all` and `schedule.read.all` **do not** globalize `.service` lifecycle mutations (LOT 23I). Out of scope → **404**.

### API

```
PATCH /api/appointments/:id/reschedule
POST  /api/appointments/:id/cancel
POST  /api/appointments/:id/no-show
POST  /api/queue/appointments/:id/no-show   → same MarkNoShow service
```

### Deferred

Queue rollback, reopen cancelled/no-show, service change on reschedule, reminders, recurring series, frontend calendars.

---

## LOT 23I — RBAC hardening (Scheduling / Appointments)

### Rules

| Topic | Rule |
|-------|------|
| Read | `schedule.read.own` / `.service` / `.all` as before |
| Manage | `schedule.manage.*` only; **`schedule.read.all` never expands manage.service** |
| Booking | `appointment.create.service` \| `.all` \| `schedule.manage.service` \| `.all` \| `*` |
| Check-in | `queue.checkin` only (unchanged) |
| Lifecycle | `appointment.*.service` stays SERVICE even with `queue.read.all` / `schedule.read.all` |
| Filters | `patientId` / `practitionerId` / `serviceId` AND with read scope (23H unchanged) |

### Out of scope / debt

- Complex SERVICE filtering of `GET /appointment-types` catalog (LOW)
- Granting `schedule.manage.own` to physicians (product decision)
- Removing legacy `POST /api/queue/appointments` (kept, same RBAC as canonical booking)
- Service-scoped `appointment_type.manage` (23M-A remains GLOBAL)
- Frontend Appointment Type admin UI (LOT 23M-B)

---

## LOT 23J — QA / Release Gate

Closes the Scheduling **release gate** (no new domain features).

### Architecture

CI workflow: `frontend/.github/workflows/e2e-release-gate.yml`.

1. **Backend unit gate** — `go test ./...` with `TEST_DATABASE_URL` cleared (skips Postgres integrations).
2. **Ticketing PostgreSQL integration gate** — ephemeral schemas.
3. **Scheduling PostgreSQL integration gate** — `go test ./internal/modules/patient_queue/ ./internal/core/rbac/ -count=1` with job `TEST_DATABASE_URL`
   (`postgres://…@127.0.0.1:5432/medcore_full_demo?sslmode=disable`).
   Tests create ephemeral `pq_<nano>` schemas and `DROP SCHEMA … CASCADE` on cleanup — **they do not mutate** the `public` schema used by migrate / `--demo-full` / Playwright.
4. Seed `--demo-full` → API → frontend unit/static → Playwright `QA_SUITE=critical` (Agenda + Patient 360 appointments included).

Local helper `npm run test:e2e:scheduling` is **not** the CI gate (Agenda-only, assumes a live API).

### SCHEDULING RELEASE READY

All must PASS:

1. PostgreSQL Scheduling integration tests (`patient_queue` + `rbac`) PASS.
2. RBAC 23I security tests PASS (same package — `rbac_hardening_23i_*`, booking routes).
3. Frontend unit / check / lint / build PASS.
4. Agenda critical E2E PASS (`e2e/agenda`).
5. Patient 360 appointments critical E2E PASS (`e2e/patient-360`).
6. `frontend/docs/QA_MATRIX.md` lists Scheduling scenarios as automated.
7. Seed QA Scheduling deterministic (`--demo-scheduling` / `--demo-full`).
8. No versioned QA artifacts (`bin/`, `test-results/`, `playwright-report/`).

### Out of scope (23J)

Schedule/exception admin UI, reminders, recurring series, waitlists, reporting, `schedule.manage.own` packs, appointment-types SERVICE catalog filter, removing legacy booking path. (Patient 360 history + Agenda deep-link delivered in **LOT 23K**.)

---

## LOT 23K — Patient 360 history + Agenda deep-link (frontend)

**Frontend-only** delivery. No backend migration, no new RBAC permission, no seed change, no backend business-code change for P0.

### Patient 360 — recent appointment history

- Extends the existing Patient 360 appointments area (sections **À venir** / **Historique**), not a new top-level tab.
- Uses existing `GET /api/appointments` with `patientId` and half-open ranges `[from, to)`.
- P0 history window: **last 31 days** only (Paris day boundary via existing Scheduling helpers).
- Classification (client-side):
  - **Upcoming:** `SCHEDULED` | `ARRIVED` | `CHECKED_IN` | `IN_PROGRESS` in the upcoming window.
  - **History:** terminal (`CANCELLED` | `NO_SHOW` | `COMPLETED`) **or** `scheduledAt < startOfToday` (stale past actives).
- History sorted **DESC** on the client. No backend `order=desc` in P0.
- Same `schedule.read.*` gating as upcoming; backend remains authoritative for visibility.

### Agenda deep-link

Contract: `/agenda?patientId=<id>` only (no clinical data in the URL).

- Valid `patientId` + booking permission → patient loaded via existing patient API and passed as `initialPatient` (preselect/lock) when the booking modal is opened by the user.
- **No** automatic booking modal open in P0 (`book=1` deferred).
- Read-only users (`schedule.read.*` without create/manage) keep Agenda readable; deep-link does **not** unlock booking.
- Invalid / inaccessible patient → non-blocking notice; Agenda remains usable; patient is not locked.

### Out of scope (23K P1+)

History >31 days, load more / infinite history, backend `order=desc`, `book=1` auto-open modal, history status filter UI, schedule/exception admin UI, reminders, recurrence, waitlist, reporting, `schedule.manage.own` packs, appointment-types SERVICE catalog filter, legacy booking endpoint removal.

---

## LOT 23N-A — Appointment notification intents (durable queue foundation)

**Backend-only** foundation for appointment reminders/notifications. **Does not deliver** SMS/email yet and **does not** hook book/reschedule/cancel (see **23N-B**).

### Domain

| Table | Role |
|-------|------|
| `appointment_notification_intents` | Durable queue row (side effect only; never mutates appointments) |
| `appointment_notification_attempts` | Delivery attempt metadata (no message body / no phone / no email) |

Distinct from Service Desk `ticketing_notifications`. In-process `MemoryBus` is **not** the reminder queue.

### Status lifecycle

`PENDING` → `PROCESSING` → `SENT` | `FAILED` (retry may reclaim `PROCESSING` → `PENDING`).
`PENDING` → `CANCELLED` | `SKIPPED`.
Terminal `SENT` / `CANCELLED` / `FAILED` / `SKIPPED` do not reopen to `PROCESSING`.

### Idempotency

Unique `(appointment_id, kind, channel, occurrence_key)`.
`occurrence_key` = UTC nanoseconds of target `scheduled_at` (deterministic; reschedule changes key).
`EnqueueNotificationIntent` is idempotent (`ON CONFLICT DO NOTHING` + read existing).

### T−24h

`ReminderSendAfterT24H(scheduledAt)` = `scheduledAt.UTC().Add(-24h)` — exactly 24 absolute hours before the canonical appointment instant. Scheduling wall-clock `Location` does not participate.

### Attempts FK

`appointment_notification_attempts.intent_id` → `appointment_notification_intents(id)` with **ON UPDATE CASCADE** and **ON DELETE RESTRICT** (preserve delivery audit; no orphan attempts; no cascade wipe).

`cmd/migrate` verifies the exact contract via PostgreSQL catalogs (`pg_constraint` / `pg_class` / `pg_attribute`): source column, referenced table/column, update action `CASCADE` (`confupdtype='c'`), and delete action either `RESTRICT` (`'r'`) or non-deferrable `NO ACTION` (`confdeltype='a'` and `condeferrable=false`). Deferrable `NO ACTION` is not equivalent (checks can be postponed). Constraint name may be GORM-generated or `fk_appt_notif_attempt_intent`.

### Channels

Domain values: `LOG`, `EMAIL`, `SMS`. Adapters in 23N-A: **Noop** / **Log** only (no network I/O, no PHI in logs).

### Payload PHI policy

Typed `NotificationPayload` requires `appointmentId` (must match the intent) and `scheduledAt` (RFC3339/RFC3339Nano, UTC-canonical). Optional type/service/clinic labels. **Forbidden:** `Appointment.Reason`, diagnosis, telephone, email, unknown keys. Empty / `{}` payloads are rejected.

### Deferred

- Real SMTP/SMS providers → later
- Patient preferences / consent / email column → out of scope
- Frontend administration UI → **23N-C2+** (depends on 23N-C1 read API)

---

## LOT 26D — Provider-neutral email transport boundary

**Architecture only** for outbound email. No Microsoft Graph, SMTP, OAuth, worker EMAIL wiring, templates, or recipient resolution in this lot.

### Flow (target)

```
business event
  → durable notification intent (23N)
  → NotificationDeliveryAdapter (channel boundary; LOG today)
  → EMAIL channel adapter (future 26F)
  → email.Transport (internal/shared/email — 26D)
  → concrete provider (future 26E)
```

### Package

`internal/shared/email` — `Message` / `Address` / `Result` / `Transport` / classified errors (`ErrNotConfigured`, `ErrTransient`, `ErrPermanent`) / in-memory `Fake`.

- Does **not** import `patient_queue` or clinical modules.
- Accepts only outbound `Message` copy; callers own privacy-approved wording (26G).
- `IdempotencyKey` is a correlation/dedup hint only — **not** exactly-once delivery.

### Lot split

| Lot | Scope |
|-----|--------|
| **26D** | Provider-neutral transport contract (this section) |
| **26E** | Microsoft 365 (or other) `Transport` implementation |
| **26F** | Wire EMAIL into durable notification worker / claim |
| **26G** | Templates + PHI/privacy outbound policy |
| **26H** | Retry / crash-after-send / idempotence hardening |

Production notification worker remains **LOG-only** until 26F.

---

## LOT 23N-B — Lifecycle integration + durable worker

### Lifecycle hooks (same TX)

Notification side effects run **inside** the appointment mutation transaction:

| Mutation | Notification effects (channel **LOG** only) |
|----------|---------------------------------------------|
| `BookAppointment` | `BOOKED` + `REMINDER_T24H` if eligible (**only while appointment status is `SCHEDULED`**). Idempotent replay of `CANCELLED` / `NO_SHOW` / other non-`SCHEDULED` rows reuses the appointment but does **not** repair/rearm notifications. |
| `RescheduleAppointment` | suppress old-occurrence active reminder; `RESCHEDULED`; new reminder if eligible |
| `CancelAppointment` | suppress active reminders (`PENDING`/`PROCESSING`); `CANCELLED` |
| `MarkNoShow` | suppress active reminders only (no fake `CANCELLED` intent) |

Legacy `CreateAppointment` delegates to `BookAppointment` (no duplicate hooks).

Atomicity: appointment row + intent changes commit or roll back together. Not via `MemoryBus`.

### Reminder eligibility

`REMINDER_T24H` only when `scheduled_at` is strictly in the future **and** `ReminderSendAfterT24H(scheduled_at) >= now`.

### Same-occurrence re-arm

Unique key remains `(appointment_id, kind, channel, occurrence_key)` (not partial).

If a `REMINDER_T24H`/`LOG` row for that key is `CANCELLED` and the reminder must be active again, **`rearmCancelledReminderTx`** explicitly transitions `CANCELLED → PENDING` (clears `cancelled_at`, refreshes `send_after` / payload). `SENT` / `FAILED` / `SKIPPED` are not silently reopened.

### Worker

Dedicated process: `cmd/notification-worker` (not started inside the API).

- Claims due `PENDING` intents for **registered** channels (`channel IN (…)`) with `send_after <= now` via `SELECT … FOR UPDATE SKIP LOCKED`, then sets `PROCESSING` + `processing_started_at` in the same short TX.
- Adapter I/O is **outside** the claim TX.
- After adapter return, delivery finalization is **one short DB transaction**: lock intent (`FOR UPDATE`, must still be `PROCESSING`) → insert attempt → `SENT` / `SKIPPED` / retry `PENDING` / `FAILED` / terminal `FAILED` → commit. Partial attempt+status is rolled back together. Finalization errors are logged; intent stays `PROCESSING` for stale recovery.
- **Feature flag:** `MEDCORE_NOTIFICATION_EMAIL_ENABLED` (shared with API). Missing/empty/`false`/`0` → disabled; `true`/`1` → enabled; other explicit values fail config load.
- **Disabled:** production adapters = **LOG** only. M365 env not required.
- **Enabled (worker):** requires `MEDCORE_M365_TENANT_ID`, `MEDCORE_M365_CLIENT_ID`, `MEDCORE_M365_CLIENT_SECRET`, `MEDCORE_M365_SENDER`. Constructs Microsoft Graph transport + canonical `patients.email` reader + EMAIL adapter. Incomplete/invalid config → **startup failure** (never silently LOG-only). Token/Graph I/O is lazy on first Send.
- **API vs worker secrets:** API uses the feature flag only for durable EMAIL lifecycle intents and does **not** need the Graph client secret. The worker alone holds `MEDCORE_M365_CLIENT_SECRET`. Keep the same enablement flag on both processes; if the worker is misconfigured while the API is enabled, EMAIL intents remain durable `PENDING` until the worker is fixed.
- Pre-send guard for `REMINDER_T24H`: skip (`PROCESSING → SKIPPED`) if appointment missing/cancelled/no-show/**completed** or occurrence key stale.
- Bounded retry: max **5** attempts; backoff 1m / 5m / 15m / 1h then `FAILED`. Permanent/invalid/not-configured email errors fail immediately. Stale `PROCESSING` (lease older than **15m**) recovery: acquire with `FOR UPDATE SKIP LOCKED`, **refresh `processing_started_at` in the same TX**, then record exactly one attempt (counts toward max) and `PENDING`+backoff or `FAILED`.
- **Exactly-once boundary:** MedCore does **not** claim exactly-once delivery for external providers. A provider may accept a message and the process may crash before finalization commits. EMAIL adapters should use provider idempotency keys where available.

### Worker container + schema ownership (LOT 26I-1 / 26I-2 / 26I-3 / 26I-4)

API, notification-worker, and migrate are **separate processes** and **separate image targets** in `backend/Dockerfile`. The worker is never started inside the API.

**`cmd/migrate` is the sole production schema owner.** API and worker do **not** AutoMigrate, Ensure\* indexes, or otherwise mutate schema at startup. Exactly **one** migration execution must succeed per release **before** API/worker replicas start.

Both runtime images (and the migrate image) install Alpine **`tzdata`** so `MEDCORE_BUSINESS_TIMEZONE` and `MEDCORE_TIMEZONE` can use real IANA zones (e.g. `Europe/Paris`, `Africa/Abidjan`) inside the container. Do not set a global `TZ` env in the image; timezone contracts remain explicit config values.

| Target | Binary / CMD | GHCR tag (CI on `main`) |
|--------|--------------|-------------------------|
| `migrate` | `./medcore-migrate` | `ghcr.io/lallene/medcore-his-migrate:latest` |
| `api` (default) | `./medcore-api` | `ghcr.io/lallene/medcore-his-api:latest` |
| `notification-worker` | `./medcore-notification-worker` | `ghcr.io/lallene/medcore-his-notification-worker:latest` |

**Production rollout order:**

1. Provide runtime configuration / secrets (never bake into images).
2. Run the migrate artifact once (`cmd/migrate` / `medcore-his-migrate`).
3. Require migration success (fail closed).
4. Start API replicas.
5. Start notification-worker replicas.

**Local development** (from `backend/`):

```bash
go run ./cmd/migrate
go run ./cmd/api
# when worker is required:
go run ./cmd/notification-worker
```

Build locally:

```bash
docker build --target migrate -t medcore-his-migrate:local .
docker build --target api -t medcore-his-api:local .
docker build --target notification-worker -t medcore-his-notification-worker:local .
```

Run migrate then worker (inject secrets at runtime — never bake them into the image):

```bash
docker run --rm \
  -e DATABASE_URL \
  -e MEDCORE_BUSINESS_TIMEZONE=UTC \
  medcore-his-migrate:local

docker run --rm \
  -e DATABASE_URL \
  -e MEDCORE_BUSINESS_TIMEZONE=UTC \
  -e MEDCORE_NOTIFICATION_EMAIL_ENABLED=false \
  -e NOTIFICATION_WORKER_POLL=2s \
  -e NOTIFICATION_WORKER_HEALTH_PORT=8081 \
  -p 8081:8081 \
  medcore-his-notification-worker:local
```

When `MEDCORE_NOTIFICATION_EMAIL_ENABLED=true`, also supply:

- `MEDCORE_M365_TENANT_ID`
- `MEDCORE_M365_CLIENT_ID`
- `MEDCORE_M365_CLIENT_SECRET`
- `MEDCORE_M365_SENDER`

Contract:

- `MEDCORE_NOTIFICATION_EMAIL_ENABLED=false` → LOG-only worker (M365 env ignored).
- `MEDCORE_NOTIFICATION_EMAIL_ENABLED=true` → full M365 config required (startup fails closed).
- Keep the same enablement flag on API and worker; drift leaves EMAIL intents `PENDING`.
- `NOTIFICATION_WORKER_POLL` unset/blank/whitespace → **2s**. Explicit malformed or non-positive values → **worker startup failure** (no silent fallback).
- Multiple API/worker replicas are safe from DDL races because they do **not** own schema mutation. Test harnesses may still AutoMigrate ephemeral schemas independently.

#### Worker health & readiness (LOT 26I-4)

The notification-worker process exposes a dedicated **stdlib `net/http`** health server (no Gin) on `0.0.0.0:<port>`:

| Env | Default | Notes |
|-----|---------|--------|
| `NOTIFICATION_WORKER_HEALTH_PORT` | `8081` | Unset/blank/whitespace → 8081 (process). Worker image also sets `ENV NOTIFICATION_WORKER_HEALTH_PORT=8081`. Explicit invalid (`<=0`, `>65535`, non-numeric) → **startup failure**. |

| Endpoint | Meaning | Success body |
|----------|---------|--------------|
| `GET /healthz` | Process / run-loop **liveness** | `ok` (200) |
| `GET /readyz` | Worker started + **DB** reachable + not shutting down | `ready` (200) |
| `GET /metrics` | Prometheus exposition (LOT **26I-5A** foundation) | Prometheus text (200) |

Failure responses for health/readiness are always generic `unavailable` (503). Probe responses intentionally expose **no** diagnostics (no DB/Graph errors, DSN, tokens, PHI, queue contents).

#### Worker metrics foundation (LOT 26I-5A)

`GET /metrics` shares `NOTIFICATION_WORKER_HEALTH_PORT` with `/healthz` and `/readyz` (no extra port, EXPOSE, or Docker HEALTHCHECK change).

Contract for 5A:

- Private Prometheus registry owned by the worker process (not the global default registry).
- No Go/process collectors and **no notification business metrics** yet (later 26I-5 slices).
- Scrape performs **no** database queries and does **not** affect `/healthz` / `/readyz`.
- Exposition must not contain patient-specific labels/data; future metrics may only use bounded enum labels (`channel`, `kind`, `outcome_class`, `provider`, `operation`) after explicit validation.
- Unauthenticated, same network surface as health probes — intended for infrastructure-network scraping (NetworkPolicy / hardening guidance primarily LOT 26I-6).

Dashboards, alerts, tick/delivery/queue instrumentation are **not** implemented in 5A.

**Liveness (`/healthz`) succeeds when** the worker Run loop has started, shutdown has not begun, and Run has not unexpectedly returned. It does **not** depend on DB availability, Microsoft Graph, M365 token acquisition, delivery success, queue depth, last Tick, or Tick duration. A long legitimate Tick alone must not fail liveness (avoids restart storms).

**Readiness (`/readyz`) succeeds when** the worker has started, shutdown has not begun, and a bounded `sql.DB.PingContext` (≈1s) succeeds. It does **not** require Graph reachable, token acquisition at probe time, successful delivery, empty queue, or a recent Tick. Temporary provider/network failure is **not** worker unready. DB unavailable after startup **is** unready. Invalid M365 configuration when EMAIL is enabled still fails at **startup** (unchanged).

**One health-port contract:** the process listener and the Docker `HEALTHCHECK` both use `NOTIFICATION_WORKER_HEALTH_PORT`. Overriding that env changes both automatically; do **not** separately override the image healthcheck for a port change. `EXPOSE 8081` is default-port metadata only and does not block another runtime port.

**Orchestration:**

| Platform | Probe |
|----------|--------|
| **Kubernetes** | liveness → `/healthz`; readiness → `/readyz` (target the configured container health port) |
| **Docker** `HEALTHCHECK` | `/readyz` on `http://127.0.0.1:${NOTIFICATION_WORKER_HEALTH_PORT}/readyz` (Docker has a single health state; a worker that cannot reach DB is not operationally useful) |

Do not confuse Docker `unhealthy` with Kubernetes liveness restart semantics. Detailed operational telemetry belongs to **26I-5**.

**Shutdown:** SIGINT/SIGTERM → mark shutting-down (readiness false immediately) → cancel worker context → existing Run cancellation semantics → graceful health HTTP `Shutdown` → exit.

**Multi-replica:** health state is **per-process only**. No DB heartbeat rows, leader election, or distributed health locks. `ClaimDue` `SKIP LOCKED` semantics are unchanged.

Deferred (later 26I slices): richer ops logging / metrics (26I-5), production M365 auth posture.

### PHI

Lifecycle payloads use `BuildNotificationPayload` only — no reason, diagnosis, telephone, email, or full Patient/Appointment objects. Adapters receive typed `NotificationPayload`; workers do not log `payload_json`.

---

## LOT 23N-C1 — Appointment notification admin read API

**Read-only** HTTP surface for scheduling administrators to inspect durable notification intents and delivery attempts. Does **not** mutate queue state, control the worker, or deliver EMAIL/SMS.

### Endpoints

| Method | Path | Purpose |
|--------|------|---------|
| GET | `/api/appointment-notification-intents` | Filtered, paginated list |
| GET | `/api/appointment-notification-intents/:id` | Intent detail + `attemptCount` |
| GET | `/api/appointment-notification-intents/:id/attempts` | Attempts ordered by `attempt_no` ASC |

**No** POST/PATCH/DELETE. **No** retry / requeue / cancel-intent / manual-send / worker health endpoints.

### RBAC

Allowed: `schedule.manage.service` \| `schedule.manage.all` \| `*` (middleware + service).

**Not** sufficient: `schedule.read.*` alone, `appointment_type.manage`, `queue.checkin`.

No dedicated `appointment_notification.read` permission in this lot.

### Service isolation

- `schedule.manage.all` / `*`: all intents joined to existing appointments.
- `schedule.manage.service`: only intents whose appointment `service_id` is in the actor’s assigned staff services (+ primary).
- Out-of-scope detail/attempts → **404** `Notification` (IDOR-safe; same pattern as schedule manage reads).

### List filters & pagination

Query: `status`, `kind`, `channel`, `appointmentId`, `sendAfterFrom`/`sendAfterTo`, `createdAtFrom`/`createdAtTo`, `page`, `limit`.

Defaults: `page=1`, `limit=50` when omitted. Explicit malformed or out-of-range `page`/`limit` → **400** (no silent clamp). Unsupported enum values and inverted ranges → **400**.

Order: `created_at DESC`, `id DESC`.

Response: `{ items, total, page, limit }`.

### PHI-safe response contract

Responses use explicit DTOs. **Never** serialize raw `payload_json`.

Payload object allow-list only: `appointmentId`, `scheduledAt`, optional `appointmentTypeName` / `serviceName` / `clinicLabel`.

**Never exposed:** appointment reason, diagnosis, telephone, email, clinical free text, unknown payload keys, raw model JSON dumps.

`patientId` is the intent’s numeric patient id (identifier only). Attempt `error` / `provider` / `providerMessageId` are operational fields only.

### Deferred

- Frontend admin UI
- Retry / requeue / cancel / manual send
- Worker observability / controls
- Real EMAIL/SMS providers and patient preferences/consent

---

## LOT 23O-A — Appointment series foundation (backend)

### Architecture

Parent **`patient_queue_appointment_series`** + **materialized** `patient_queue_appointments` occurrences.

- One atomic PostgreSQL transaction creates: series row, every occurrence, appointment histories, and existing **23N** notification intents (`BOOKED` + eligible `REMINDER_T24H`).
- Any occurrence failure rolls back the **entire** series (no partial visibility).
- Public `BookAppointment` unchanged: still opens its own transaction; series materialization calls internal `bookAppointmentTx` inside the outer series TX (**no nested GORM transactions**).
- Waitlist, EMAIL/SMS, MemoryBus, and treating 23N intents as generic jobs are **out of scope** (see 23O-B / 23O-C / later lots).

### P0 recurrence limits

| Rule | Constraint |
|------|------------|
| `freq` | `WEEKLY` only |
| `byWeekdays` | non-empty, unique, Go `time.Weekday` **0=Sunday … 6=Saturday** (same as StaffWorkingSchedule) |
| `intervalWeeks` | `>= 1` |
| end condition | **`count` XOR `until`** (exactly one) |
| `count` | 1…**52** |
| horizon | hard **12 months** from anchor — rules that would exceed → **400** (never silently truncate) |
| infinite | forbidden |

### Practitioner

`practitionerId` is **required** and **fixed** for the entire series (no auto-assign across occurrences).

### Timezone / DST

- Series `timezone` must be a valid **IANA** name.
- Expansion preserves anchor **local wall-clock** time; output is ascending UTC.
- **Spring gap:** non-existent local civil times are **rejected** (no silent Go normalization).
- **Fall-back ambiguity:** deterministic via Go `time.Date` (earlier of the two instants).

### PHI boundary

Create/get DTOs expose series metadata + occurrence summaries (`id`, `index`, `scheduledAt`, `scheduledEndAt`, `status`, `practitionerId`).

**Not stored on the series / not returned:** diagnosis, clinical notes, phone/email copies, `Appointment.Reason` by default.

### Idempotency

- Series: unique `(created_by, idempotency_key)` when key present; same semantics → reuse; different semantics → **409**.
- Occurrences: deterministic key `series:{id}:occ:{index}` plus unique `(series_id, series_occurrence_index)`.

### API / RBAC

| Method | Path | Authority |
|--------|------|-----------|
| `POST` | `/api/appointment-series` | `appointment.create.service\|all` **or** `schedule.manage.service\|all` **or** `*` (same as booking). `schedule.read.*` / queue alone **cannot** create. |
| `GET` | `/api/appointment-series/:id` | `schedule.read.own\|service\|all` with backend service/own isolation; out-of-scope → **404** (anti-enumeration). |

### 23N interaction

Each successful occurrence runs the same book hooks as single booking (`BOOKED` + optional `REMINDER_T24H`) inside the series transaction. Distinct appointment IDs / scheduled instants → distinct notification keys.

### Deferred (out of scope for 23O)

Waitlist, EMAIL/SMS providers, automatic workers beyond the existing 23N LOG worker.

---

## LOT 23O-B — Appointment series lifecycle (cancel)

### Status machine

Persisted: **`ACTIVE` | `CANCELLED`** only. No PAUSED / COMPLETED. No reactivation.

| Op | Effect |
|----|--------|
| Cancel entire | All `SCHEDULED` occurrences → `CANCELLED` via 23E semantics; series → `CANCELLED`; `Version++` |
| Cancel future from index N (inclusive) | `SCHEDULED` with `series_occurrence_index >= N` cancelled; series → `CANCELLED` iff none remain `SCHEDULED`, else stays `ACTIVE`; always `Version++` on success |
| Already `CANCELLED` + cancel entire | **200** no-op (no version bump) |
| Already `CANCELLED` + cancel future | **409** |
| Stale `expectedVersion` | **409** |

Single-occurrence cancel/reschedule still use **23E** endpoints. Reschedule of an occurrence whose parent series is `CANCELLED` → **409**.

### Transaction / locks

```
BEGIN
  [series lifecycle idempotency advisory 230406 if key]
  SELECT series FOR UPDATE + expectedVersion OCC
  assert appointment.cancel.* service scope
  lock patient → practitioners ASC
  SELECT target appointments FOR UPDATE (id ASC)
  cancelAppointmentTx × N (LocksHeld; same TX; 23N cancel hooks)
  UPDATE series status/version
COMMIT
```

Namespaces: create idempotency **`230405`**; series lifecycle **`230406`**; occurrence lifecycle remains **`230404`**.

`cancelAppointmentTx` never opens a nested GORM transaction.

### API / RBAC

```
POST /api/appointment-series/:id/cancel
POST /api/appointment-series/:id/cancel-future
```

Body (cancel): `{ expectedVersion, reason?, idempotencyKey? }`
Body (cancel-future): `{ expectedVersion, fromOccurrenceIndex | fromAppointmentId, reason?, idempotencyKey? }`

Authority: **`appointment.cancel.service|all`** only (not `schedule.manage.*`, not `schedule.read.*`).

### 23N

Each cancelled occurrence runs `applyCancelNotificationIntentsTx` in the same TX (suppress active reminders + `CANCELLED` LOG intent).

---

## LOT 23O-C — Appointment series update / reschedule (future)

Update an **ACTIVE** series from a cutoff so **future `SCHEDULED`** occurrences adopt new series metadata and/or a regenerated timing segment. Historical and operational rows stay untouched. Single-occurrence cancel/reschedule remains **23E**.

### Mutable vs immutable

| Mutable (from cutoff) | Immutable |
|----------------------|-----------|
| `practitionerId`, `appointmentTypeId` (duration follows type) | `patientId`, `serviceId`, `freq` |
| `intervalWeeks`, `byWeekdays`, `timezone` | `status`, `createdBy`, create `idempotencyKey` |
| `count` **XOR** `until`, `anchorStartAt` (required when regenerating timing/recurrence) | identity / audit timestamps |

There is **no location** field on the 23O-A series model — not introduced here.

### Effective cutoff

Exactly one of:

- `fromOccurrenceIndex` (≥ 1, inclusive), or
- `fromAppointmentId` (must belong to the series; cutoff = that occurrence’s index)

Only `SCHEDULED` occurrences with `index >= cutoff` that still **match the pre-update series rule** (time / practitioner / type / duration) are updated. Excluded:

- any non-`SCHEDULED` status (`ARRIVED`, `IN_PROGRESS`, `COMPLETED`, `CANCELLED`, `NO_SHOW`, …)
- indexes `< cutoff` (past relative to the selected boundary)
- **23E exceptions**: `SCHEDULED` rows individually rescheduled/cancelled so they diverge from the rule expansion

### Regeneration semantics

- **Meta-only** (practitioner and/or appointment type): in-place update of updatable rows; series metadata updated; `anchorStartAt` unchanged.
- **Timing / recurrence** (`anchorStartAt`, interval, weekdays, timezone, count/until): require `anchorStartAt` as the first start of the **updated segment**. Reuses `ExpandWeeklyOccurrences` (same generator as 23O-A). Prefer **in-place** reschedule of updatable rows (unique `(series_id, series_occurrence_index)` still held by cancelled rows). Surplus updatable rows are cancelled via `cancelAppointmentTx`; extra slots use `bookAppointmentTx`. On success, series `anchorStartAt` / end condition reflect the regenerated segment; `Version++` **once**.

### OCC / CANCELLED

- Request **must** include `expectedVersion`.
- Stale version → **409**, zero mutation.
- Series `CANCELLED` → **409** (no reactivation).
- Advisory lifecycle namespace **`230406`** when `idempotencyKey` is present (same pattern as 23O-B).

### Conflicts

Availability / patient overlap reuse existing booking primitives. Any conflicting regenerated occurrence → **full TX rollback** and the existing scheduling **409** conflict contract.

### Transaction

```
BEGIN
  [series lifecycle advisory 230406 if key]
  SELECT series FOR UPDATE + expectedVersion OCC
  assert appointment.reschedule.* | schedule.manage.* service scope
  resolve cutoff; build plan; lock all series appointments
  lock patient → practitioners ASC
  update/cancel/create future SCHEDULED (23E/23N hooks in-TX)
  UPDATE series metadata + version
COMMIT
```

No nested GORM transactions.

### API / RBAC

```
PATCH /api/appointment-series/:id
```

Example request (timing regen from index 2):

```json
{
  "expectedVersion": 1,
  "fromOccurrenceIndex": 2,
  "practitionerId": 42,
  "anchorStartAt": "2026-12-15T10:00:00Z",
  "reason": "clinic room change",
  "idempotencyKey": "series-up-1"
}
```

Example meta-only:

```json
{
  "expectedVersion": 2,
  "fromAppointmentId": 1001,
  "appointmentTypeId": 7
}
```

Response: same series DTO as create/get (`AppointmentSeriesDTO`).

Authority (same as occurrence reschedule): **`appointment.reschedule.service|all`** **or** **`schedule.manage.service|all`** **or** `*`.

Not sufficient alone: `schedule.read.*`, `appointment.cancel.*`, `queue.checkin`. Out-of-scope service → **404**.

### 23N

Each occurrence whose schedule changes runs `applyRescheduleNotificationIntentsTx` or cancel/book hooks inside the same TX. Intent enqueue remains idempotent; rollback leaves no partial outbox rows.

---

## LOT 23O-D — Appointment series read (detail + occurrences)

Read APIs expose the full series state for agenda UI (no PHI beyond existing series DTO fields).

### Endpoints

| Method | Path | Authority |
|--------|------|-----------|
| `GET` | `/api/appointment-series/:id` | `schedule.read.own\|service\|all` (same as 23O-A); out-of-scope → **404** |
| `GET` | `/api/appointment-series/:id/occurrences` | same read authority |

Detail response remains `AppointmentSeriesDTO` (metadata, rule, status, version, occurrences).

Occurrences response:

```json
{
  "seriesId": 12,
  "status": "ACTIVE",
  "version": 2,
  "items": [ /* SeriesOccurrenceDTO[] */ ]
}
```

### Occurrence `kind` (derived)

| Kind | Meaning |
|------|---------|
| `RULE` | `SCHEDULED` and still matches the current series rule |
| `EXCEPTION_RESCHEDULED` | `SCHEDULED` but diverged via 23E single-occurrence reschedule |
| `EXCEPTION_CANCELLED` | status `CANCELLED` (23E or series cancel) |
| `OPERATIONAL` | `ARRIVED` / `CHECKED_IN` / `IN_PROGRESS` / `COMPLETED` / `NO_SHOW` / … |

Not persisted — computed on read from status + rule alignment (same divergence helper as 23O-C).

Cancelled series remain readable (status `CANCELLED`); no reactivation.

---

## LOT 23O-E — Agenda UI (recurring series)

Frontend agenda integrates 23O-A/B/C/D without a second recurrence engine.

- **Create:** booking modal mode « Série récurrente » → `POST /api/appointment-series` (fixed practitioner, weekdays, interval, count XOR until, IANA timezone = agenda site).
- **Identify:** appointment cards/details show series badge when `seriesId` is present; inline recurrence summary from GET series.
- **Edit:** « Reporter ce RDV » (23E) vs « Modifier ce RDV et suivants » (23O-C + `expectedVersion`); OCC 409 refreshes series data (no silent overwrite).
- **Cancel:** « ce RDV » / « ce RDV et suivants » / « toute la série » with confirmations for bulk ops.
- **Detail:** series modal lists occurrences and derived `kind` values from 23O-D.
- RBAC mirrors backend helpers (`appointment.create.*`, `appointment.reschedule.*`, `appointment.cancel.*`, `schedule.read.*`).

### Patient 360

`PatientAppointments` reuses the same Agenda series helpers (`series.ts` / `series-actions.ts`) and Series* modals — view / edit-this / edit-future / cancel-this / cancel-future / cancel-entire. No duplicated series business logic. After successful mutations (and on OCC 409), Patient 360 refreshes appointment lists + series state without a full-page reload.

---

## LOT 23O-F/G — Final QA, E2E, documentation (close-out)

23O is **complete**. No further product surface for recurring series in this lot.

### Invariants (architecture)

| Invariant | Guarantee |
|-----------|-----------|
| One recurrence engine | `ExpandWeeklyOccurrences` only (create + edit-future) |
| 23E vs 23O | Single-occurrence lifecycle = 23E; series lifecycle = 23O |
| 23N consistency | Book/cancel/reschedule hooks + outbox intents stay in the same TX as series mutations |
| No nested GORM TX | Series TX calls `bookAppointmentTx` / `cancelAppointmentTx` with locks already held |
| Advisory namespaces | occurrence lifecycle **230404**; series create **230405**; series lifecycle **230406** |
| OCC | `expectedVersion` on cancel-future / cancel-entire / PATCH; stale → **409**, zero mutation |
| CANCELLED | Terminal — no reactivation; updates against CANCELLED → **409** |
| History / exceptions | Operational + past rows preserved; 23E exceptions excluded from edit-future regeneration |
| RBAC | Backend authoritative; out-of-scope → **404**; FE helpers only hide unauthorized actions |

### E2E

Playwright `e2e/agenda/series.spec.ts` (critical): UI create + detail, edit-future OCC conflict feedback, cancel entire, Patient 360 cancel-future, read-only RBAC gating. Reuses `e2e/agenda/fixtures.ts`.
