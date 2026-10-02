# Clinical Timeline — API Contract (LOT28E-E)

Authoritative documentation of the **shipped** Patient360 / medical-record clinical timeline.
Runtime code remains the source of truth; this document mirrors current behavior only.

Related implementation:

- Model: `internal/modules/medical_records/model.go` (`MedicalTimelineEvent`)
- Read + projection: `internal/modules/medical_records/service.go`, `timeline_authority.go`
- Routes: `GET /api/medical-records/:recordId/timeline`, `GET /api/patients/:id/medical-summary`
- FE labels (presentation only): `frontend/.../clinical-timeline-labels.ts`

---

## 1. Patient360 composition authority

| Permission | Meaning |
|---|---|
| `patients.360.read` | **Shell / entry** capability for the Patient360 UI only. Does **not** grant universal access to every clinical or financial domain. |
| `patients:read` | Demographics / patient list authority. |
| Domain permissions | Remain authoritative for each module (`consultations.read`, `medical_records.read`, `performed_acts.read`, `billing.read`, `insurance.authorization.read`, etc.). |

Patient360 composes module surfaces behind domain permissions. Frontend hiding is presentation only; backend RBAC is authoritative.

---

## 2. Timeline base authority

| Permission | Meaning |
|---|---|
| `medical_records.read` | Required to call timeline / medical-summary endpoints. Grants visibility of **timeline occurrence rows** after read-time projection. |

Timeline is a longitudinal / composed clinical-history feed. It is **not** automatic authority to receive full detail from every source domain (billing, insurance, performed acts, …).

---

## 3. Endpoints

### `GET /api/medical-records/{recordId}/timeline`

- **RBAC:** `medical_records.read` (403 if missing).
- **Response:** JSON **array** of `MedicalTimelineEvent` (not wrapped in a data envelope).
- **Order:** `event_date DESC`, then `id DESC`.
- **Projection:** every element is passed through `ProjectTimelineEventsForCaller` before serialization.

### `GET /api/patients/{id}/medical-summary`

- **RBAC:** `medical_records.read`.
- Includes a recent `timeline` array (last 20) with the **same** projection rules.

### MedicalRecord bootstrap

`GET /api/patients/{id}/medical-record` **GetOrCreates** the dossier (`GetOrCreateMedicalRecord`).
Patient creation does **not** automatically create a MedicalRecord.

---

## 4. Response schema (`MedicalTimelineEvent`)

JSON field names as serialized today:

| Field | Type | Notes |
|---|---|---|
| `id` | number | Row id |
| `medical_record_id` | number | Timeline home |
| `patient_id` | number | |
| `event_type` | string | Open string (see inventory). Not a closed OpenAPI enum. |
| `category` | string | Open string. Not a closed OpenAPI enum. |
| `title` | string | Display title (may be generic after redaction) |
| `description` | string | May be `""` after redaction (field always present; not omitted) |
| `department_id` | number \| **null** | Pointer; JSON `null` when unset |
| `reference_type` | string | May be `""` after redaction |
| `reference_id` | number \| **null** | Pointer; JSON `null` when unset / redacted |
| `severity` | string | Typically `"info"` |
| `event_date` | string (RFC3339) | Business / clinical occurrence time |
| `created_by` | number | Actor who caused the write (authoritative path) |
| `created_at` | string (RFC3339) | Persistence timestamp |

### Why EventType / Category stay open strings

Timeline writers span many modules and evolve independently. Closing them as OpenAPI enums would falsely freeze vocabulary. Known values are documented below; clients must tolerate unknown types (FE falls back to `title` / generic label).

---

## 5. Time semantics

| Field | Meaning |
|---|---|
| `event_date` (**EventDate**) | Business / clinical occurrence timestamp. |
| `created_at` (**CreatedAt**) | When the timeline row was persisted. |

They are **not** interchangeable. Retrospective acts may have `event_date` ≪ `created_at`.

### PerformedAct

| Event | EventDate source |
|---|---|
| `performed_act_performed` | `PerformedAct.PerformedAt` |
| `performed_act_voided` | `PerformedAct.VoidedAt` |

---

## 6. Reference semantics

`reference_type` / `reference_id` identify a source aggregate **when projection permits exposure**.

They are **not** a guarantee that the caller may navigate to or fetch that aggregate.
Redacted events may clear `reference_type` to `""` and `reference_id` to `null`.

Current FE timeline navigation remains consultation-specific. There is **no** generic PerformedAct navigation from the timeline UI (LOT28F deferred).

---

## 7. Read-time projection (AUTH)

Projection is **backend-authoritative**, applied on every timeline / medical-summary read.
Stored rows are never mutated by projection (value-copy).

### Base

`medical_records.read` → occurrence visible (subject to domain redaction below).

### Billing (LOT28E-B2-B / AUTH-C)

Without `billing.read`, financial events project to:

- `event_type` → `billing_event`
- generic title « Événement de facturation »
- `description` → `""`
- `reference_type` → `""`, `reference_id` → `null`

### Insurance (LOT28E-B2-B / AUTH-C)

Without `insurance.authorization.read`, insurance events project to:

- `event_type` → `insurance_event`
- generic title « Événement d'assurance »
- `description` → `""`
- refs cleared as above

### PerformedAct (LOT28E-B3 / AUTH-B)

Without `performed_acts.read`, PA events keep:

- `event_type` (`performed_act_performed` / `performed_act_voided`)
- `event_date`
- generic title (« Acte réalisé » / « Acte annulé »)
- `category` = `performed_act`

and clear:

- `description` (ActLabel snapshot)
- `reference_type` / `reference_id`
- no VoidReason, no financial / insurance detail through timeline

With `performed_acts.read`:

- safe ActLabel snapshot may remain in `description`
- `reference_type` = `PerformedAct`, `reference_id` = PA id

Still **never** through timeline: `VoidReason`, invoice amounts, insurer shares.

---

## 8. Transaction / fail-closed policy (not universal)

Timeline writers do **not** share one transaction policy.

| Writer family | Policy (shipped) |
|---|---|
| **PerformedAct create / void (B3)** | Same business TX as PA transition. Timeline insert failure rolls back the PA create or void. Requires an existing MedicalRecord (fail-closed; PA does **not** auto-create MR). |
| **consultation_created (B2-C1)** | Best-effort / post-commit — must **not** be described as fail-closed. |
| **Generic CMR updates (B2-A)** | Best-effort — must **not** be described as fail-closed. |
| Documents, vitals, billing, insurance, lab, imaging, … | Follow each module’s existing writer semantics (see module code). |

Do not generalize B3 same-TX guarantees to every timeline event.

### MedicalRecord prerequisite (PerformedAct)

Post-B3 successful PA create/void requires a MedicalRecord so the timeline row has a home.
MR is lazily bootstrapped via `GET /patients/:id/medical-record` (GetOrCreate).
Missing MR → conflict / fail-closed on PA create/void. PA does not silently create MR.
MR is **not** guaranteed at patient creation.

### Historical compatibility (PerformedAct)

Pre-B3 PerformedAct rows may legitimately have **no** PA timeline events.
No historical backfill. Timeline is **not** a complete immutable reconstruction of all pre-writer activity.
Timeline reads do not fabricate missing PA events.

### Idempotency boundary

B3 guarantees one required timeline event for each **successful** B3 PA transition under tested TX/state invariants.
It does **not** provide global exactly-once business commands, duplicate-PA prevention, or global timeline uniqueness.

### Source-domain coexistence

Lab / imaging workflow timeline events may coexist with PA occurrence events. They are distinct layers, not deduplicated.

---

## 9. Canonical event vocabulary (writers)

Exact strings from current backend writers. Clients must tolerate additional future types.

### Documents (B1)

| event_type | category | reference_type |
|---|---|---|
| `document_added` | `document` | `MedicalDocument` |
| `document_archived` | `document` | `MedicalDocument` |

Archive is soft lifecycle (not hard delete). **`document_deleted` is not emitted.**

### Vital signs

| event_type | category | reference_type |
|---|---|---|
| `vital_signs_recorded` | `vital_signs` | `vital_sign` |

Frontend may still **read**-map legacy alias `vital_sign_added` for label compatibility. It is **not** a current canonical writer event.

### Medical record / CMR / consultation clinical

| event_type | category | notes |
|---|---|---|
| `common_medical_record_updated` | `medical_record` | B2-A best-effort |
| `allergy_added` | `allergy` | |
| `medical_history_added` | `medical_history` | |
| `consultation_created` | `consultation` | B2-C1 best-effort / post-commit |
| `consultation_status_changed` | `consultation` | |
| `exam_requested` | `exam` | |
| `medication_prescribed` | `prescription` | |
| `soap_updated` | `consultation` | |
| `specialty_data_updated` | `consultation` | |

`consultation_completed` / `consultation_cancelled` appear in FE label maps and may exist in seed/demo data; they are **not** listed here as active production writers unless independently confirmed in module code.

### PerformedAct (B3)

| event_type | category | reference_type | titles |
|---|---|---|---|
| `performed_act_performed` | `performed_act` | `PerformedAct` | Acte réalisé |
| `performed_act_voided` | `performed_act` | `PerformedAct` | Acte annulé |

Performed description (authorized): ActLabel catalogue snapshot only. Void description: empty (no VoidReason).

### Billing (stored types; may project to `billing_event`)

`invoice_issued`, `payment_received`, `invoice_paid`, `invoice_cancelled` — category `billing`, ref `billing_invoice`.

### Insurance (stored types; may project to `insurance_event`)

`insurance_authorization_created`, `insurance_authorization_submitted`, `insurance_authorization_approved`, `insurance_authorization_partially_approved`, `insurance_authorization_rejected`, `insurance_authorization_cancelled`, `insurance_authorization_act_linked` — category `insurance`, ref `insurance_authorization`.

### Laboratory

`lab_order_created`, `lab_sample_collected`, `lab_analysis_started`, `lab_result_entered`, `lab_result_validated`, `lab_order_cancelled` — category `laboratory`, ref `laboratory_order`.

### Imaging

`imaging_order_created`, `imaging_scheduled`, `imaging_started`, `imaging_report_drafted`, `imaging_report_validated`, `imaging_cancelled`, `imaging_report_closed` — category `imaging`, ref `imaging_order`.

### Hospitalization

`hospitalization_created`, `hospitalization_admitted`, `hospitalization_discharged`, `hospitalization_cancelled` — category `hospitalization`, ref `hospitalization`.

### Pharmacy

`medication_dispensed`, `medication_partially_dispensed` — category `prescription`, ref `pharmacy_dispensation`.

### Projection-only event types (not stored writers)

| event_type | When |
|---|---|
| `billing_event` | Unauthorized billing detail |
| `insurance_event` | Unauthorized insurance detail |

---

## 10. Synthetic examples

### Authorized `performed_act_performed`

```json
{
  "id": 101,
  "medical_record_id": 9,
  "patient_id": 3,
  "event_type": "performed_act_performed",
  "category": "performed_act",
  "title": "Acte réalisé",
  "description": "Suture cutanée",
  "department_id": null,
  "reference_type": "PerformedAct",
  "reference_id": 55,
  "severity": "info",
  "event_date": "2026-01-02T10:00:00Z",
  "created_by": 12,
  "created_at": "2026-01-02T10:00:01Z"
}
```

### Redacted `performed_act_performed` (no `performed_acts.read`)

```json
{
  "id": 101,
  "medical_record_id": 9,
  "patient_id": 3,
  "event_type": "performed_act_performed",
  "category": "performed_act",
  "title": "Acte réalisé",
  "description": "",
  "department_id": null,
  "reference_type": "",
  "reference_id": null,
  "severity": "info",
  "event_date": "2026-01-02T10:00:00Z",
  "created_by": 12,
  "created_at": "2026-01-02T10:00:01Z"
}
```

### `performed_act_voided` (authorized)

```json
{
  "id": 102,
  "medical_record_id": 9,
  "patient_id": 3,
  "event_type": "performed_act_voided",
  "category": "performed_act",
  "title": "Acte annulé",
  "description": "",
  "department_id": null,
  "reference_type": "PerformedAct",
  "reference_id": 55,
  "severity": "info",
  "event_date": "2026-01-03T08:15:00Z",
  "created_by": 12,
  "created_at": "2026-01-03T08:15:00Z"
}
```

### Document lifecycle

```json
{
  "event_type": "document_added",
  "category": "document",
  "title": "Document médical ajouté",
  "reference_type": "MedicalDocument",
  "reference_id": 77
}
```

```json
{
  "event_type": "document_archived",
  "category": "document",
  "title": "Document médical archivé",
  "reference_type": "MedicalDocument",
  "reference_id": 77
}
```

---

## 11. Security notes

- Timeline projection is enforced by the API. Frontend filters / labels do not protect data.
- Do not treat `reference_id` as navigable without the matching domain permission.
- Examples above are synthetic; never paste real patient / invoice / insurer identifiers into docs.
