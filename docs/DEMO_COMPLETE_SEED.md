# Jeu de données de référence `--demo-complete`

`seed --demo-complete` génère une instance MedCore complète et cohérente : 365 jours
d'activité, au moins 1 500 patients et tous les workflows opérationnels, chacun dans ses
principaux états. Le seed `--demo-full` existant reste inchangé.

## Exécution

```bash
# Base dédiée VIDE et migrée (go run ./cmd/migrate), nom autorisé (medcore_full_demo / medcore_lot12_demo)
export MEDCORE_ALLOW_DEMO_SEED=1
export MEDCORE_DEMO_SEED_PASSWORD='<mot de passe de démonstration, 12 à 72 caractères>'
go run ./cmd/seed --demo-complete
```

Paramètres optionnels :

| Variable | Défaut | Rôle |
|---|---|---|
| `MEDCORE_DEMO_COMPLETE_ANCHOR` | dernier jour dont 12:00 est passé (fuseau métier) | dernier jour simulé (`AAAA-MM-JJ`) ; refusé si son instant simulé (12:00) est dans le futur |
| `MEDCORE_DEMO_COMPLETE_DAYS` | 365 | nombre de jours simulés (14 à 730) |
| `MEDCORE_DEMO_COMPLETE_PATIENTS` | 1500 | population (40 à 20 000) ; tous les volumes sont proportionnels |
| `MEDCORE_DEMO_COMPLETE_SEED` | 20260101 | graine pseudo-aléatoire |

Une exécution complète prend environ 4 minutes sur un poste de développement. Elle produit
environ 370 000 lignes ré-ancrées dans le temps.

## Sécurité

- **Garde-fou commun** (`requireDemoSeed`) : `APP_ENV ≠ production` (sauf l'exception
  pré-V1 explicite), base autorisée, Neon refusé (nom et hôte), `MEDCORE_ALLOW_DEMO_SEED=1`,
  mot de passe obligatoire et sans valeur par défaut.
- **Garde-fou de jeu de données** : la génération n'a lieu que sur une base **sans aucun
  patient**. Une base qui contient des patients non créés par `--demo-complete` (dossiers
  réels, ancien seed démo) est refusée, quels que soient les opt-in.
- **Comptes** : création seule. Un compte existant n'est jamais modifié, réactivé ni
  rétrogradé.
- **Aucune notification externe** : le worker de notifications tourne avec des adaptateurs
  sûrs uniquement (LOG ; e-mail sans réseau). Les adresses des patients sont sur le domaine
  réservé `.example`.
- **Aucun secret dans les logs** : ni mot de passe, ni `DATABASE_URL`.

## Principes

1. **Services métier réels.** Chaque état est atteint par les opérations de l'application,
   par exemple ORDERED → SAMPLE_PENDING → SAMPLE_COLLECTED → IN_PROGRESS → RESULT_ENTERED →
   VALIDATED. Le câblage des services est celui de `cmd/api`, producteurs d'actes compris.
   Les règles métier s'appliquent donc : séparation des tâches, PEC en attente qui bloque la
   facturation, espèces uniquement en caisse, périmètres de service, etc.
2. **Parcours plutôt que tables.**
   - Réservation → check-in → triage → médecin → consultation.
   - Consultation → labo / imagerie / pharmacie / hospitalisation.
   - Actes réalisés → PEC → facture → paiements → créances → règlements.
3. **Ré-ancrage temporel** (`complete/timewarp.go`). Les services horodatent avec
   `time.Now()` et n'exposent pas d'horloge injectable.
   - Chaque étape s'exécute dans une fenêtre de temps réel associée à son instant simulé.
   - Après chaque journée simulée, les colonnes temporelles écrites dans ces fenêtres sont
     réécrites vers l'instant simulé ; l'ordre causal est conservé.
   - Les valeurs planifiées fournies en temps simulé ne sont jamais déplacées : créneaux de
     RDV, validités, dates de naissance, etc.
   - C'est la seule écriture directe systématique sur les tables métier. Elle ne change
     jamais un statut, un montant, une relation ou un numéro.
4. **Instant simulé unique.** Le jeu de données représente l'établissement le jour d'ancrage
   à **12:00** (fuseau métier). Cet instant ne dépend que de la configuration, jamais de
   l'heure de lancement ; le générateur (horizon) et le validateur l'utilisent tous les deux.
   Une ancre explicite dont l'instant de 12:00 n'est pas encore atteint est refusée : les
   services comparent leurs règles à l'horloge réelle.
5. **Le futur n'est pas exécuté.** Une étape postérieure à l'instant simulé n'est jamais jouée.
   L'entité reste alors dans son état courant, ce qui donne naturellement des objets en
   attente ou en cours, et une journée du jour vivante.

## Déterminisme

- Une même configuration (graine, ancre, volumes) sur une base vide produit le même jeu de
  données logique :
  - flux aléatoires dérivés par domaine (et par demande de labo / imagerie) ;
  - ordonnancement stable `(instant, ordre d'insertion)` ;
  - aucun parcours de map Go ;
  - tri canonique des actes facturables et des demandes, sans dépendre de l'ordre de lecture
    de PostgreSQL.
- Le résultat ne dépend pas de l'heure de lancement : l'instant simulé est l'ancre à 12:00.
- Seuls les horodatages diffèrent d'une exécution à l'autre, de quelques millisecondes,
  car le ré-ancrage conserve l'écart réel entre les écritures d'une même étape.
- Limite : les numéros de pièces que les services construisent avec l'année réelle (factures,
  tickets de file, bons…) changent si l'on génère une même ancre une autre année civile.

## Ré-exécution

- Relancer sur une base générée par `--demo-complete` **ne génère rien** : la commande
  rejoue seulement la validation.
- Pour régénérer, recréer la base, la migrer et relancer.

## Validation

À la fin du run, `DEMO COMPLETE VALIDATION` affiche :
- les volumes ;
- PASS / FAIL par domaine ;
- `OVERALL` (PASS seulement si tous les contrôles critiques passent).

La commande sort en erreur si `OVERALL = FAIL`.

Contrôles effectués :
- volumes et période couverte ;
- statuts attendus, en distinguant :
  - les **invariants historiques**, critiques. Exemples : chaque bon PEC sorti de `draft` a
    sa transition dans `workflow_histories`, le cycle complet du bon est présent dans
    l'historique, aucune session de caisse d'un jour antérieur n'est restée ouverte ;
  - les **états instantanés**, évalués à l'instant simulé. Exemple : une session de caisse
    `OPEN` est exigée seulement si une caisse est ouverte à cet instant (06:30–22:00), ce
    qui est toujours le cas à 12:00 ; les bons encore en `draft`, `submitted` ou
    `controlled` sont informatifs ;
- comptes RBAC ;
- cohérences (labo validé avec prélèvement et résultats, facture payée sans solde,
  remboursement exécuté avec exécution, séparation des tâches respectée, CASH toujours en
  session…) ;
- intégrité (orphelins, dates futures) ;
- génération réelle des 5 PDF de consultation ;
- documents imprimables ;
- pagination.

## Limites constatées (le seed n'invente pas ces fonctionnalités)

**États jamais produits par le code**
- RDV `ARRIVED` ; étape de file `RECEPTION` (le check-in mène directement en
  `WAITING_TRIAGE`) ; `ON_HOLD` / `REDIRECTED`.
- Statuts financiers de file `EXEMPT` / `BLOCKED`.
- Prélèvement `REJECTED` ; bordereau `ACKNOWLEDGED` ; dispensation `CANCELLED`.

**Notifications** : SMS n'est pas un canal du cycle de vie des notifications.

**Défauts et incohérences produit**
- Les capabilities d'imagerie (XRAY / ULTRASOUND / CT) sont portées par le profil mais non
  contrôlées par le module imagerie : aucun refus par capability n'est possible.
- Aucune fonction RBAC ne porte `insurance.voucher.*` : le workflow des bons PEC n'est
  utilisable qu'avec un compte `*` (administrateur).
- Être assigné à un ticket de support n'en donne pas la visibilité : un agent ne voit que
  les tickets de son service principal.
- `act_catalog.Create` enregistre `billable`, `insuranceEligible` et `isActive` à `true`
  même quand ils sont envoyés à `false` (GORM `default:true`). Le seed crée l'acte puis le
  modifie, comme un opérateur.

**Non générés**
- Pièces jointes de support : écrites par le handler HTTP, sans API de service.
- Exports CSV / Excel : inexistants dans le produit.
