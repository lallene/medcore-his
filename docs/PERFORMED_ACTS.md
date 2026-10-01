# Performed Acts — Actes réalisés (LOT27C)

Durable record of an act **actually performed** for a patient.

## Boundaries

| Concept | Owner | Notes |
|---|---|---|
| **ActCatalog** | LOT27B | What *can* be done |
| **PerformedAct** (`performed_acts`) | LOT27C | What *was* done for a patient |
| Clinical producers | **LOT27D** | Auto-mapping from consult / lab validate / imaging start — see `CLINICAL_PRODUCERS.md` |
| Insurance / PEC | **LOT27E** | Explicit submit: `ReferenceType=PERFORMED_ACT` → existing authorization lifecycle |
| Financial split | **LOT27F** | Decide/Calculate on authorization: RequestedAmount → InsuranceAmount / PatientAmount |
| Billing | **LOT27G** | Invoice lines from PerformedAct via existing Tariff + optional PEC allocation |

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

Means the catalogue marked the act as potentially eligible to **enter** a PEC workflow.
It does **not** mean the patient is covered, that an authorization exists, that the insurer
pays 100%, that the patient share is zero, or that `BasePrice` is the final insurer amount.

## LOT27E — PerformedAct → PEC

An eligible `PerformedAct` may be submitted into the **existing** `InsuranceAuthorization`
lifecycle via:

`POST /api/insurance/authorizations`

with:

- `referenceType = PERFORMED_ACT`
- `referenceId = performed_acts.id`
- **explicit** `patientCoverageId` (never auto-selected / never principal fallback)

Creates a PEC in `DRAFT`. Eligibility requires `Status=PERFORMED` and `InsuranceEligible=true`.
VOIDED acts cannot create a new PEC.

`RequestedAmount` remains optional at create and is **not** final insurer payment.

## LOT27F — Insurance / patient financial split

Decision basis and split are owned by existing `Calculate` / `Decide` on
`InsuranceAuthorization` (no rewrite in LOT27F):

```
RequestedAmount
  → ApprovedRate and/or ApprovedAmount
  → CeilingAmount (optional clamp)
  → InsuranceAmount
  → PatientAmount = RequestedAmount − InsuranceAmount (2 decimal places)
```

Rules:

- **`RequestedAmount`** is the monetary basis for Decide. It is optional at Create and
  **mandatory** at Decide. It is **not** auto-derived from `PerformedAct.BasePrice` or
  `Quantity`.
- **`contractRate` / `PatientCoverage.CoverageRate`** is informational on read responses
  and is **not** automatically applied to the split.
- **`APPROVED` and `PARTIALLY_APPROVED` use the same Calculate math**; the status label does
  not change the formula. Operators choose the label; inputs (rate/fixed/ceiling) drive amounts.
- **`REJECTED`**: `InsuranceAmount = 0`, `PatientAmount = RequestedAmount`. Approval terms
  (`ApprovedRate`, `ApprovedAmount`, `CeilingAmount`) are cleared so they are not exposed as
  effective. Rejection reason is required.
- Final split is persisted on the authorization row. Decide does not create invoices,
  allocations, receivables, or payments.
- Void/open-PEC reconciliation remains **LOT27H**.

## LOT27G — PerformedAct → Billing

Existing `POST /api/billing/invoices` accepts `actType=PERFORMED_ACT` and
`referenceId=performed_acts.id` (no new endpoint).

```
PerformedAct (PERFORMED ∧ Billable)
  → Tariff (ActType=PERFORMED_ACT, optional ReferenceID=ActCatalogEntryID)
  → gross = round(Quantity × Tariff.UnitPrice)
  → optional PEC via FindAuthorizationForAct(PERFORMED_ACT, id)
  → insurance allocation (existing financialCoverage)
  → patient = gross − insurance
```

Rules:

- **Tariff is the billing price authority.** `PerformedAct.BasePrice` is catalogue reference
  only and is **never** used as invoice `UnitPrice`.
- **Quantity** comes from `PerformedAct.Quantity`.
- **`Billable`** controls billing eligibility; **`InsuranceEligible`** only gates PEC entry
  (self-pay remains allowed when Billable even if not insurance-eligible).
- **VOIDED** acts are not billable.
- Duplicate active billing is blocked by `billable_key=PERFORMED_ACT:{id}` and existing
  `ux_billing_active_billable_key` (`is_active=true`). Cancel releases the key.
- Medication billing remains dispensation-based (`MEDICATION_DISPENSATION:{id}`); unchanged.
- Billing still discovers PEC via the patient's **activeCoverage** then
  `FindAuthorizationForAct` (pre-existing multi-coverage limitation; not redesigned here).
- Void/open-PEC reconciliation remains **LOT27H**.

**Dual-reference risk:** historical PECs may still reference `CONSULTATION` / `LABORATORY` /
`IMAGING` / … while a newer PEC references `PERFORMED_ACT` for related care. Coexistence is
allowed; destructive deduplication is deferred. VOIDED-after-PEC reconciliation is **LOT27H**.

Medication PEC remains prescription-based (`MEDICATION` → `consultation_prescriptions`).

## API / RBAC

- `GET/POST /api/performed-acts`, `GET /api/performed-acts/:id`, `POST /api/performed-acts/:id/void`
- `performed_acts.read` / `performed_acts.create` / `performed_acts.void`
- Schema owned by `cmd/migrate` only
