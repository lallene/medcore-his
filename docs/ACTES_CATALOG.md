# ACTES — Catalogue (LOT27B)

Canonical hospital act catalogue / référentiel des actes.

## Boundaries

| Concept | Owner | Notes |
|---|---|---|
| **ActCatalog** (`act_catalog_entries`) | LOT27B | Clinical/commercial nomenclature + current base price |
| **Billing Tariff** (`billing_tariffs`) | Billing (unchanged) | Authoritative invoice pricing today |
| **MedicalExam** | Consultations | Lab/imaging exam names — independent |
| **Performed patient act** | Future LOT27C | Not implemented in 27B |
| **Medication / presentation** | Pharmacy | **Excluded** from ActCatalog in 27B |

```
ActCatalog  ≠  BillingTariff
ActCatalog  ≠  MedicalExam
ActCatalog  ≠  PerformedAct
```

## Semantics

- **Code**: stable, uppercase, globally unique, immutable after create; not recycled after deactivation.
- **BasePrice**: current catalogue list price (integer minor units, default currency XOF). Changing it must not rewrite historical invoice lines (lines already snapshot `unit_price` from tariffs).
- **Billable**: act *may* participate in billing workflows.
- **InsuranceEligible**: act *may* enter a future PEC workflow. It does **not** mean a specific insurer covers the act (27E/27F).

## Categories (bounded)

`CONSULTATION` | `LABORATORY` | `IMAGING` | `HOSPITALIZATION` | `PROCEDURE` | `OTHER`

`MEDICATION` is not a catalogue category in 27B.

## API / RBAC

- `GET/POST /api/act-catalog`, `GET/PUT /api/act-catalog/:id`
- Permissions: `act_catalog.read`, `act_catalog.manage`
- Schema owned by `cmd/migrate` only (no runtime AutoMigrate).

## Future ownership (no implementation here)

| Lot | Owns |
|---|---|
| 27C | Performed patient acts + price/code snapshots |
| 27E | Insurance / PEC submission |
| 27F | Financial allocation |
| 27G | Billing integration with catalogue |
| 27H | Clinical producers / exam mappings |
| 27I | ACTES frontend admin |
