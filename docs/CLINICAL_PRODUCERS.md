# Clinical producers → PerformedAct (LOT27D)

Automatic creation of durable `PerformedAct` rows from eligible clinical workflows,
**only when producers are enabled** and explicit `ProducerMap` rows are configured.

Preserves the LOT27C ledger as the single record of acts actually performed.

## Boundary diagram

```
Clinical workflow
      |
      | performed/completed  (only if PRODUCERS ENABLED)
      v
ActCatalog mapping (act_catalog_producer_maps)
      |
      v
PerformedAct
      |
      +------> LOT27E Insurance / PEC   [PERFORMED_ACT reference — explicit submit]
      |
      +------> LOT27G Billing           [PERFORMED_ACT → Tariff + optional PEC]
```

Medication remains on its separate prescription/dispensation chain.

**LOT27E note:** PEC creation for a performed act is **manual/explicit** with a chosen
`PatientCoverageID`. Clinical producers never auto-create authorizations. Dual clinical +
`PERFORMED_ACT` authorizations may coexist historically; no destructive migration.

## Enablement (deployment safety)

Environment flag (operational, not a secret):

`PERFORMED_ACT_PRODUCERS_ENABLED`

| Value | Behaviour |
|---|---|
| unset / empty / `false` / `0` (**default**) | Clinical workflows run normally; **no** automatic PerformedAct; map admin API + readiness available |
| `true` / `1` | Fail-closed producers active: missing/invalid map rolls back the clinical transition |

**Enabled + bad/missing mapping = fail closed.**
**Disabled = no automatic PerformedAct creation.**

## Deployment sequence

```
1. MIGRATE (schema only — no mapping DATA)
2. START API WITH PRODUCERS DISABLED (default)
3. CONFIGURE ActCatalog entries (existing admin)
4. CONFIGURE ProducerMaps via API (backend-only)
5. GET .../producer-maps/readiness → ready=true
6. ENABLE PERFORMED_ACT_PRODUCERS_ENABLED=true
7. RESTART / REDEPLOY
8. VERIFY readiness again
9. CLINICAL TRAFFIC
```

`cmd/migrate` creates the `act_catalog_producer_maps` table and indexes only.
It does **not** invent business mappings.

## Implemented producers (when enabled)

| SourceType | Trigger transition | SourceID | ClinicalKey |
|---|---|---|---|
| `CONSULTATION` | status → `completed` (UpdateStatus or queue Complete) | `consultations.id` | `""` (canonical empty) |
| `LABORATORY` | order → `VALIDATED` | `laboratory_orders.id` | exact `medical_exams.code` |
| `IMAGING` | order `Start` → `IN_PROGRESS` (sets `performed_at`) | `imaging_orders.id` | exact `medical_exams.code` |

A request/order/schedule alone does **not** create a PerformedAct.

### Imaging residual note

`Start` is the domain performance event (`performed_at` / `performed_by`). Cancel is only allowed from `ORDERED`/`SCHEDULED`. If an exam is abandoned after Start, the PerformedAct remains and requires explicit LOT27C void / future reconciliation — no automatic void.

## Producer map admin API (backend-only, no frontend)

| Method | Path | Permission |
|---|---|---|
| GET | `/api/performed-acts/producer-maps` | `performed_acts.producer_map.read` |
| GET | `/api/performed-acts/producer-maps/readiness` | `performed_acts.producer_map.read` |
| GET | `/api/performed-acts/producer-maps/:id` | `performed_acts.producer_map.read` |
| POST | `/api/performed-acts/producer-maps` | `performed_acts.producer_map.manage` |
| PUT | `/api/performed-acts/producer-maps/:id` | `performed_acts.producer_map.manage` |
| POST | `/api/performed-acts/producer-maps/:id/deactivate` | `performed_acts.producer_map.manage` |

No hard DELETE.

Roles with map manage: `DIRECTEUR_ADMINISTRATIF`, `DIRECTEUR_MEDICAL`.
Clinical performers do **not** receive map management merely because they can complete acts.

## Mapping rules

- Deterministic `(source_type, clinical_key)` → `act_catalog_entry_id`
- No fuzzy / label / price matching
- Active map requires active ActCatalog entry at configuration time
- Producer execution revalidates catalogue activity (fail-closed when enabled)

## Readiness

`GET .../producer-maps/readiness` reports:

- consultation: canonical empty clinical_key map
- laboratory: every **active** `medical_exams` row whose category matches laboratory Materialize filters (`examcategories.IsLaboratory`, shared with laboratory module)
- imaging: every **active** `medical_exams` row with category `Imagerie` (`examcategories.IsImaging`, shared with imaging module)

If there are zero lab/imaging exams in those categories, those producers are ready for that universe (nothing to map) once consultation is configured.

Does not invent mappings. Does not make `/healthz` fail when producers are disabled.

## Idempotency

Partial unique index `ux_performed_acts_source` on `(source_type, source_id)` when both set.
`EnsureFromProducer` is idempotent (unique race → existing row via SAVEPOINT on PostgreSQL).

## Snapshot

Manual `Create` and `EnsureFromProducer` share `applyCatalogSnapshot` (LOT27C fields).

## Deferred / non-goals

**Hospitalization** is not a LOT27D producer (`SourceType` has no `HOSPITALIZATION`).
Admission/stay events do not auto-create PerformedActs here — deferred to a later lot.

No insurance/PEC **automation**, billing invoices, medication producers, frontend, LOT27F+.

LOT27E adds explicit `PERFORMED_ACT` PEC submission only (see `PERFORMED_ACTS.md`).

## Schema ownership

- `act_catalog_producer_maps` via `cmd/migrate` AutoMigrate
- `ux_performed_acts_source` via `EnsurePerformedActIndexes`
- No runtime AutoMigrate on API boot
