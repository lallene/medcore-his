# LOT 19 / 20 — Patient Queue / Clinical Flow / Doctor Worklist

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

## LOT 20 — Doctor Worklist / visibilité médecin

Acteur **doctor-only** (`queue.doctor.read` sans `queue.reception.read` /
`queue.triage.read` / `queue.read.all` / `*`) :

- Visible : `WAITING_DOCTOR`, `DOCTOR_IN_PROGRESS` (+ `COMPLETED` si demandé
  explicitement sur List/Get)
- Non visible : `RECEPTION`, `WAITING_TRIAGE`, `TRIAGE_IN_PROGRESS`

Appliqué côté backend sur `GET /tickets` (List force les stages post-triage ;
filtre stage pré-triage → **400**) et `GET /tickets/:id` (pré-triage → **404**,
pas de fuite d'existence). Accueil / Infirmier / `queue.read.all` inchangés.

`GET /api/queue/doctor/worklist` expose uniquement `WAITING_DOCTOR` et
`DOCTOR_IN_PROGRESS` (filtre serveur). Les étapes triage ne sont jamais retournées.

Enrichissement read-only depuis les modèles existants :
- `patients` (identité, âge, sexe, téléphone)
- `vital_signs` (résumé + drapeaux anormaux)
- `allergies` / `medical_histories` (panneau détail)
- motif RDV (`patient_queue_appointments.reason`) ou historique check-in walk-in

Prise en charge atomique inchangée (`doctor_taken_by IS NULL` + `RowsAffected`).
Clôture via `POST /tickets/:id/complete` (workflow LOT 19).

Frontend Design System : `/queue`, `/queue/reception`, `/queue/triage`,
`/queue/doctor` (worklist + panneau clinique), `/queue/[id]`.
