# Clinical Flow — LOT 22

Parcours clinique intégré : file patient → prise en charge médecin → consultation existante → décision → clôture ticket.

## Schéma du parcours

```
Accueil (check-in)
    ↓
Triage (infirmier)
    ↓
WAITING_DOCTOR
    ↓ POST /api/queue/tickets/:id/doctor/take
Médecin prend en charge (+ consultation in_progress si demandée)
    ↓
DOCTOR_IN_PROGRESS
    ↓ Workspace consultation (/consultations/:id)
Lab / Imagerie / Prescription / Hospitalisation (modules existants)
    ↓
Décision médicale (disposition SOAP)
    ↓ POST /api/queue/tickets/:id/complete
Consultation completed + Ticket COMPLETED (transaction)
```

## ACTUEL vs CIBLE (audit LOT 22)

| Étape | Module existant | Route frontend | API backend | État avant LOT 22 | Manque | Action LOT 22 |
|-------|-----------------|----------------|-------------|-------------------|--------|----------------|
| Check-in | patient_queue | `/queue`, `/queue/reception` | `POST /api/queue/check-in/walk-in` | ✅ LOT 19 | — | Réutilisé |
| Triage | patient_queue | `/queue/triage` | `POST triage/take`, `triage/complete` | ✅ | Vitals UI partiel | Réutilisé (API vitals existante) |
| File médecin | patient_queue | `/queue/doctor` | `GET /api/queue/doctor/worklist` | ✅ LOT 20 | — | Réutilisé |
| Prise en charge | patient_queue | `/queue/doctor` | `POST doctor/take` | ✅ atomique | Consultation `draft`, pas de lien vitals | **in_progress + link vitals + réutilisation** |
| Lien ticket ↔ consultation | patient_queue + consultations | — | `patient_queue_tickets.consultation_id` | Partiel (1-way) | Reverse lookup | **GET by-consultation, queueTicketId sur GET consultation** |
| Handoff triage | vital_signs, allergies | Panel médecin | `Get`, worklist enrich | ✅ lecture | — | Réutilisé |
| Workspace clinique | consultations | `/consultations/:id` | `GET/PUT/PATCH consultations` | ✅ | Bouton terminer mort | **Terminer prise en charge + barre contexte** |
| Patient 360 | patients | `/patients/:id` | `GET patients/360` | ✅ | Pas d’indicateur file | **PatientActiveCareBanner** |
| Décision médicale | consultation_soaps | SOAP tab | disposition SOAP | ✅ champ | Pas dans clôture | **CompleteRequest.disposition** |
| Clôture | patient_queue + consultations | worklist + consultation | `POST complete` | Ticket seul | Consultation orpheline | **Clôture transactionnelle** |
| RBAC | rbac | guards LOT 21 | middleware | ✅ | — | `queue.doctor.take`, `consultations.update` réutilisés |
| Historique | patient_queue_history | `/queue/:id` | writeHistory | ✅ | — | Réutilisé |
| Sécurité médecin B | patient_queue | — | Complete | Partiel | Pas ownership strict | **assertDoctorCanComplete** |

## Existant / réutilisé (non dupliqué)

- Patient 360, consultations, vital_signs, allergies, antécédents, timeline, lab, imagerie, pharmacie, hospitalisation
- Stages file : `WAITING_DOCTOR` → `DOCTOR_IN_PROGRESS` → `COMPLETED`
- Permissions : `queue.doctor.read`, `queue.doctor.take`, `consultations.read/create/update`
- `patient_queue_history` pour audit transitions
- Design System LOT 18 (composants UI existants)

## Ajouté par LOT 22

### Backend

| Fichier | Changement |
|---------|------------|
| `patient_queue/service.go` | `TakeDoctor` : consultation `in_progress`, réutilisation, lien `vital_signs.consultation_id` |
| `patient_queue/service.go` | `Complete` : clôture consultation dans la même transaction, disposition SOAP |
| `patient_queue/service.go` | `GetByConsultationID`, `GetActiveTicketForPatient` |
| `patient_queue/dto.go` | `CompleteRequest` |
| `patient_queue/routes.go` | `GET tickets/by-consultation/:id`, `GET patients/:patientId/active-ticket` |
| `consultations/model.go` | `QueueTicketID` (lecture, `gorm:"-"`) |
| `consultations/repository.go` | Enrichissement `queueTicketId` sur `FindByID` |

### Frontend

| Fichier | Changement |
|---------|------------|
| `ConsultationWorkspace.svelte` | Terminer prise en charge, disposition, RBAC bouton |
| `ClinicalFlowContextBar.svelte` | Barre contexte + liens navigation |
| `PatientActiveCareBanner.svelte` | Indicateur prise en charge sur Patient 360 |
| `queue/doctor/+page.svelte` | Redirection consultation après take, Continuer |
| `lib/api/queue.ts` | Payload complete, active-ticket, by-consultation |

### Tests

- `postgres_integration_test.go` : sync consultation, ownership, réutilisation, lookups
- `e2e/clinical-flow/clinical-flow.spec.ts` : QA-CLINICAL-FLOW-001, QA-CLINICAL-FLOW-RBAC-001

## Règles transactionnelles

1. **TakeDoctor** : `UPDATE` atomique `WAITING_DOCTOR` + `doctor_taken_by IS NULL` → 409 si conflit.
2. **Complete** : dans une transaction Postgres — consultation `completed` puis ticket `COMPLETED` ; échec consultation → ticket non clôturé.
3. **Idempotence Complete** : ticket déjà `COMPLETED` → succès sans double historique.
4. **Ownership** : seul `doctor_taken_by` (ou `*` / `queue.read.all`) peut clôturer.

## Règles RBAC

| Action | Permission |
|--------|------------|
| Voir file médecin | `queue.doctor.read` (+ scope service) |
| Prendre / clôturer | `queue.doctor.take` |
| Ouvrir consultation | `consultations.read` |
| Modifier consultation | `consultations.update` |
| Indicateur Patient 360 | `queue.doctor.read` ou `patients.360.read` |

Backend = autorité. Frontend masque actions interdites (`canCompleteCare`).

## APIs

| Méthode | Route | Description |
|---------|-------|-------------|
| POST | `/api/queue/tickets/:id/doctor/take` | Prise en charge (+ `createConsultation`) |
| POST | `/api/queue/tickets/:id/complete` | Clôture ticket + consultation |
| GET | `/api/queue/tickets/by-consultation/:id` | Ticket lié à une consultation |
| GET | `/api/queue/patients/:patientId/active-ticket` | Ticket actif médecin pour un patient |
| GET | `/api/consultations/:id` | + `queueTicketId` |

## E2E QA (LOT 22.1)

| Test | Patient | Finance |
|------|---------|---------|
| QA-CLINICAL-FLOW-001 | `P-DEMO-010` | `GET /api/queue/finance/:id` → `CLEAR` (facture DEMO entièrement payée) |
| QA-CLINICAL-FLOW-RBAC-001 | `P-DEMO-007` | `financeOverride: true` (même workflow que QA-QUEUE-SMOKE-001) |

Constantes triage : `POST /api/medical-records/:recordId/vital-signs` puis `completeTriage` avec `vitalSignsId`.

Playwright `webServer` : preview `:4173` avec `reuseExistingServer: true` (ne pas tuer un preview sain).

## P0 / P1 restants

| Priorité | Item |
|----------|------|
| P2 | UI triage : saisie constantes avant `completeTriage` |
| P2 | Activer entrée sidebar `/consultations` (`soon: true`) |
| P3 | Lien `/queue/:id` → consultation (texte seul aujourd’hui) |
