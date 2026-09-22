# Performed Acts — Actes réalisés (LOT27C)

Durable record of an act **actually performed** for a patient.

## Boundaries

| Concept | Owner | Notes |
|---|---|---|
| **ActCatalog** | LOT27B | What *can* be done |
| **PerformedAct** (`performed_acts`) | LOT27C | What *was* done for a patient |
| Clinical producers | Future LOT27D | Auto-mapping from consult/lab/imaging/hosp |
| Insurance / PEC | Future LOT27E | Submission references **PerformedAct** |
| Financial split | Future LOT27F | Insurer / patient shares |
| Billing | Future LOT27G | Invoice integration |

```
ActCatalog  ≠  PerformedAct
PerformedAct ≠  InvoiceLine
PerformedAct ≠  InsuranceAuthorization
PerformedAct ≠  MedicalExam
MEDICATION stays outside this chain
```

## Snapshot semantics

On create, the server copies from the active ActCatalog entry:

code, label, description, category, basePrice, currency, billable, insuranceEligible

Later catalogue edits **must not** rewrite historical PerformedAct rows.

`BasePrice` on a PerformedAct is the **catalogue reference price at performance time**.
It is not an invoice amount, tariff amount, insurer approved amount, or copay.

## Lifecycle

`PERFORMED` → `VOIDED` (terminal). Soft void only; no hard delete. Voided rows remain readable.

## InsuranceEligible snapshot

Means the catalogue marked the act as potentially eligible for a future PEC workflow.
It does **not** mean the patient is covered or that an authorization exists.

## API / RBAC

- `GET/POST /api/performed-acts`, `GET /api/performed-acts/:id`, `POST /api/performed-acts/:id/void`
- `performed_acts.read` / `performed_acts.create` / `performed_acts.void`
- Schema owned by `cmd/migrate` only
