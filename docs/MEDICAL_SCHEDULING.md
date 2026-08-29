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

| Concept | Role | 23A status |
|---------|------|------------|
| **Appointment** | Booked interval for a patient | Extended (`scheduled_at` / `scheduled_end_at`, type) |
| **Working Schedule** | Recurring staff hours | Deferred 23B |
| **Schedule Exception** | Absences / blocks | Deferred 23B |
| **Availability** | Derived free intervals | Deferred 23C |
| **Generated Slot** | Ephemeral search result | Not persisted |
| **Queue Ticket** | Operational clinical journey | Unchanged (LOT 19–22) |

## Appointment interval semantics

- `scheduled_at` = **start inclusive**
- `scheduled_end_at` = **end exclusive** → half-open `[start, end)`
- Invariant: `end > start` when end is set
- Adjacent intervals `09:00–09:30` and `09:30–10:00` do **not** overlap
- Legacy / queue-only creates may leave `scheduled_end_at` **NULL** (readable; not invented silently)
- When `appointment_type_id` is set and end omitted, end = start + `default_duration_minutes`

## AppointmentType

Table `patient_queue_appointment_types`:

- Unique stable `code`
- `default_duration_minutes` **must be > 0**
- Optional `service_id` constraint
- **Not** `consultation_reasons` (clinical motifs)

## Practitioner identity

`expected_doctor_id` → **`users.id`** (canonical practitioner).

Do not introduce doctors/practitioners tables.

## Lifecycle vs Queue

**Before check-in** — appointment owns scheduling status:

`SCHEDULED` → (`ARRIVED` transient) → `CHECKED_IN` | `NO_SHOW` | (`CANCELLED` reserved)

**After check-in** — Queue owns operational flow (`WAITING_TRIAGE` → … → `COMPLETED`).

Appointment status may sync for context: `IN_PROGRESS` (doctor take), `COMPLETED` (ticket complete).

Do not treat appointment status as a substitute for ticket stages.

## History

Table `patient_queue_appointment_history` (append-only), separate from `patient_queue_history`.

Events: `CREATED`, `CHECKED_IN`, `NO_SHOW`, `IN_PROGRESS`, `COMPLETED` (+ reserved: `CONFIRMED`, `RESCHEDULED`, `PRACTITIONER_CHANGED`, `CANCELLED`).

`actor_user_id` comes from JWT / Access — never from frontend body.

Not written to Medical Timeline.

## Timezone (23A)

- Storage: timestamps via GORM/`time.Time` as **UTC** (`ScheduledAt.UTC()`, `time.Now().UTC()`)
- `ListAppointmentsToday` uses UTC day truncation
- Broader wall-clock / facility timezone policy deferred to 23B/23C
- Do not mix local wall-clock with UTC in new code paths

## Overlap protection

23A establishes interval columns and indexes.

**PostgreSQL EXCLUDE / overlap locks belong to LOT 23D.**  
A unique `(doctor, scheduled_at)` is **not** sufficient for overlapping intervals.

## Indexes (23A)

- `(service_id, scheduled_at)`
- `(expected_doctor_id, scheduled_at)`
- `(patient_id, scheduled_at)`
- `(status, scheduled_at)`
- `(appointment_type_id, scheduled_at)`

## Future phases

| Phase | Focus |
|-------|--------|
| 23B | Working schedules + exceptions |
| 23C | Availability engine |
| 23D | Booking concurrency / overlap |
| 23E | Reschedule / cancel workflows |
| 23F | Richer Queue check-in UX |
| 23G | Reception / practitioner calendars |
| 23H | Patient 360 upcoming RDV |
| 23I | `appointments.*` / `schedule.*` RBAC |
| 23J | QA / release gate |
