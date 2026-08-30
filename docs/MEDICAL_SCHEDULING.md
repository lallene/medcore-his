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

- Env: **`MEDCORE_TIMEZONE`** (IANA name)
- Config field: `config.Timezone`
- Runtime: `internal/core/scheduling` (`Location()`)
- **Default:** `UTC` (consistent with current appointment UTC storage until a deployment sets an explicit zone)

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

Startup/`cmd/migrate` drop obsolete global `ux_pq_appt_idempotency` if present, then create the caller-scoped index.

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

Table `patient_queue_appointment_types`: unique `code`, `default_duration_minutes` > 0.

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

`EnsureTicketIndexes` is a **hard** API startup invariant (`Module.Register` panics on failure; `cmd/migrate` fatals). Duplicate historical `appointment_id` values fail clearly without silent repair.

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

## Future phases

| Phase | Focus |
|-------|--------|
| 23G | Reception / practitioner calendars (frontend) |
| 23H | Patient 360 upcoming RDV |
| 23J | QA / release gate |

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
