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
| **Availability** | Derived free intervals | Deferred 23C |
| **Generated Slot** | Ephemeral search result | Not persisted |
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

### RBAC (LOT 21 catalog)

| Permission | Intent |
|------------|--------|
| `schedule.read.own` | Own schedules (JWT user) |
| `schedule.read.service` | Assigned services |
| `schedule.read.all` | Global read |
| `schedule.manage.own` | Manage own (must still be assigned to service) |
| `schedule.manage.service` | Manage within service scope |
| `schedule.manage.all` | Global manage |

Staff packs (minimum): physicians → `schedule.read.own`; ACCUEIL → `schedule.read.service`; DIRECTEUR_MEDICAL → read.all + manage.service; DIRECTEUR_ADMINISTRATIF → read.all + manage.all.

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

**No** `GET /availability` (23C).

### LOT 23C domain contract

Service methods (not HTTP DTOs):

- `ListApplicableWorkingWindows(practitionerID, serviceID, fromDate, toDate) ([]WorkingWindow, error)`
- `ListApplicableExceptions(practitionerID, serviceID, from, to) ([]ScheduleException, error)`
- `ExceptionPrecedenceNegativeWins() bool` → true

Future equation:

```
Working Schedule + Schedule Exceptions − Existing Appointments = Availability
```

23B implements only the first two inputs.

### Boundary with Queue / Worklist

After patient arrival, Queue / check-in / finance / triage / Doctor Worklist / LOT 22 remain authoritative. Schedules do not feed the worklist.

---

## Appointment interval semantics (23A)

- `scheduled_at` = **start inclusive**
- `scheduled_end_at` = **end exclusive** → half-open `[start, end)`
- Invariant: `end > start` when end is set
- Adjacent intervals `09:00–09:30` and `09:30–10:00` do **not** overlap
- Legacy / queue-only creates may leave `scheduled_end_at` **NULL**
- When `appointment_type_id` is set and end omitted, end = start + `default_duration_minutes`

## AppointmentType

Table `patient_queue_appointment_types`: unique `code`, `default_duration_minutes` > 0.

## Practitioner identity

`expected_doctor_id` / schedule `practitioner_id` → **`users.id`**.

## Lifecycle vs Queue

**Before check-in** — appointment owns scheduling status.

**After check-in** — Queue owns operational flow.

## Appointment history (23A)

Table `patient_queue_appointment_history` — separate from schedule audit.

## Overlap protection (appointments)

**PostgreSQL EXCLUDE / booking locks belong to LOT 23D.**

## Indexes

**23A appointments:** service/doctor/patient/status/type + `scheduled_at`.

**23B schedules:** `(practitioner_id, service_id, weekday)`, validity/active; exceptions range; audit entity.

## Future phases

| Phase | Focus |
|-------|--------|
| 23C | Availability engine |
| 23D | Booking concurrency / overlap |
| 23E | Reschedule / cancel workflows |
| 23F | Richer Queue check-in UX |
| 23G | Reception / practitioner calendars |
| 23H | Patient 360 upcoming RDV |
| 23J | QA / release gate |
