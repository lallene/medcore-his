# MedCore Service Desk

Le module `internal/modules/ticketing` fournit un Service Desk interne, sans dépendance SaaS et sans capacité d’exécution système.

## Architecture et données

Les tickets, commentaires, affectations, historique, catégories, SLA et métadonnées de pièces jointes utilisent les tables additives `ticketing_*`. Staff et Organization restent les référentiels d’identité et de service. Aucun JWT, secret ou contenu clinique n’est copié automatiquement. Les références patient/consultation sont facultatives et limitées à des identifiants contrôlés.

## Workflow

`NEW → TRIAGED/ASSIGNED → IN_PROGRESS → WAITING_USER/WAITING_THIRD_PARTY → RESOLVED → CLOSED`.
Une reprise passe explicitement par `RESOLVED → REOPENED`; `CLOSED` et `CANCELLED` sont terminaux. Les transitions sont verrouillées et validées dans une transaction PostgreSQL.

Types : `INCIDENT`, `REQUEST`, `ACCESS_REQUEST`, `HARDWARE`, `NETWORK`, `APPLICATION`, `OTHER`.
Impact : `INDIVIDUAL`, `SERVICE`, `DEPARTMENT`, `FACILITY`. Urgence : `LOW`, `MEDIUM`, `HIGH`, `CRITICAL`. La combinaison calcule `P4` à `P1`; un agent autorisé peut la requalifier.

## SLA

La configuration `ticketing_slas` est la source de vérité. Les valeurs initiales sont P1 15 min/2 h, P2 30 min/4 h, P3 4 h/24 h, P4 24 h/72 h. Les échéances sont figées sur le ticket et recalculées lors d’une requalification. `first_response_at`, `resolved_at` et `closed_at` permettent les KPI sans métrique inventée.

## Permissions

Tous les utilisateurs actifs reçoivent `ticket.create`, `ticket.read.own`, `ticket.comment`. Les fonctions `SUPPORT_AGENT` et `SUPPORT_MANAGER` ajoutent les vues de service/globales, commentaire interne, affectation, workflow et audit. Le backend filtre toujours les ressources : un ticket étranger est exposé comme introuvable et un commentaire `INTERNAL` n’est jamais sérialisé au demandeur.

## API

- `POST/GET /api/tickets`, `GET/PATCH /api/tickets/:id`
- `POST/GET /api/tickets/:id/comments`
- `POST /api/tickets/:id/assign`, `POST /api/tickets/:id/workflow`
- `GET /api/tickets/:id/history`
- `GET /api/ticketing/categories`, `GET /api/ticketing/kpis`

La liste est paginée et filtre référence/texte, statut, priorité, type, catégorie, assignation et dépassement SLA.

## Numérotation et concurrence

La référence lisible (`INC|REQ|ACC-AAAA-NNNNNN`) est générée dans la transaction avec un verrou consultatif PostgreSQL par préfixe/année, puis protégée par un index unique.

## Sécurité et exploitation

Le module n’accepte ni commande shell ni SQL utilisateur. Les données d’auteur viennent du contexte JWT. Les changements sensibles sont conservés dans `ticketing_history`; aucune suppression physique n’est exposée. Le dataset `--demo-full` ajoute uniquement des clés `*-DEMO-*`, sous les gardes DEMO existantes.

## Matrice fonctionnelle

| Feature | Permission | Rôle | Précondition | Endpoint | QA | Criticité |
|---|---|---|---|---|---|---|
| Créer/voir son ticket | `ticket.create/read.own` | utilisateur actif | JWT | `/api/tickets` | Smoke | P0 |
| Vue périmètre | `ticket.read.service/all` | support | Staff/Service | `GET /api/tickets` | Critical | P0 |
| Commentaire interne | `ticket.comment.internal` | support | ticket visible | `/comments` | Critical négatif | P0 |
| Affectation | `ticket.assign` | support | agent/queue | `/assign` | Full | P1 |
| Résoudre/fermer/rouvrir | permissions dédiées | support/manager | transition valide | `/workflow` | Critical/Full | P0 |
| SLA/KPI | `ticket.read.service/all` | support | configuration SLA | `/ticketing/kpis` | Full | P1 |
| Audit | `ticket.audit.read` | manager | ticket visible | `/history` | Full | P1 |

## QA

Les tests Go couvrent matrice de priorité, délais déterministes, transitions et isolation propriétaire. Les tests frontend couvrent les transitions/labels. La QA Playwright doit valider création, isolation inter-utilisateurs, 401/403, commentaires internes et workflow sur une base PostgreSQL 17 isolée.
