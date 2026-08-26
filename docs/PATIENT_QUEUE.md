# LOT 19 — Patient Queue / Clinical Flow

## Architecture

Module backend dédié `internal/modules/patient_queue` (séparé du ticketing IT).

Tables GORM AutoMigrate :
- `patient_queue_appointments` — source RDV (pas de module agenda existant)
- `patient_queue_tickets` — parcours après check-in validé (`Q-YYYY-NNNNNN`)
- `patient_queue_history` — audit des transitions

Workflow strict : après check-in validé le ticket démarre en `WAITING_TRIAGE`
puis `TRIAGE_IN_PROGRESS` → `WAITING_DOCTOR` → `DOCTOR_IN_PROGRESS` → `COMPLETED`
(+ `CANCELLED` / `ON_HOLD` / `NO_SHOW` / `REDIRECTED` dans le graphe ; endpoints
dédiés ON_HOLD/REDIRECTED non exposés).

Finance gate : `EvaluateFinance` lit billing/receivables (CLEAR / PAYMENT_REQUIRED /
INSURANCE_PENDING) — override audité.

Périmètre service : lectures et mutations appliquent le même scope
(`assertCanAccessService` / `assertServiceInScope`). `queue.read.all` autorise
le cross-service si la permission de mutation est aussi présente. Hors périmètre
→ 404 (ticket/RDV) ou 403 (serviceId explicite).

Constantes (`vitalSignsId`) : vérification same-patient obligatoire via
`vital_signs.patient_id`. Si ticket et constantes portent tous deux un
`consultation_id`, ils doivent coïncider. Sinon la visite/encounter n'est pas
contrainte davantage (limite documentée).

Fenêtre retard RDV : constante fixe ±15 minutes (`AppointmentWindow`).

Frontend Design System : `/queue`, `/queue/reception`, `/queue/triage`,
`/queue/doctor`, `/queue/[id]`.
