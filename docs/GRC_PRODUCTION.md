# GRC — préparation de production

Ce document décrit comment déployer plus tard le domaine GRC. Il n'autorise aucun déploiement, aucune migration et aucun e-mail depuis le poste qui le lit.

Le travail GRC de ce workspace n'est pas un artefact de release : il n'est pas commité. Une mise en production future exige d'abord une revue, un commit et un identifiant d'artefact immuable. Ne pas déployer `latest` seul.

## Architecture réelle

```text
frontend statique (SvelteKit, image sirv)
  → API Go (cmd/api, image cible api)
  → PostgreSQL

cmd/migrate                 schéma, une fois, avant les processus
cmd/notification-worker     rendez-vous, processus séparé
cmd/grc-reminder-worker     rappels GRC, processus séparé
Microsoft 365               transport e-mail du worker GRC seulement
```

L'API n'exécute pas `AutoMigrate` et ne démarre pas le worker GRC. Le dépôt ne contient ni Kubernetes, ni systemd, ni reverse proxy. TLS, en-têtes de sécurité et terminaison HTTPS sont hors du processus Go.

Images présentes dans `backend/Dockerfile` :

```text
migrate
notification-worker
grc-reminder-worker
api                     (cible par défaut)
```

Le workflow `.github/workflows/backend-docker.yml` construit les quatre cibles depuis le même checkout. Il ne s'exécute que sur un push vers `main`. Une pull request ne publie rien. Il n'y a pas de déclencheur manuel ni de publication sur un tag Git. Aucun job de ce workflow ne déploie.

Registre : `ghcr.io`, authentification `GITHUB_TOKEN`, permissions `contents: read` et `packages: write`.

```text
ghcr.io/lallene/medcore-his-api
ghcr.io/lallene/medcore-his-notification-worker
ghcr.io/lallene/medcore-his-migrate
ghcr.io/lallene/medcore-his-grc-reminder-worker
```

Chaque image reçoit deux tags du même build : `latest` et le SHA complet du commit (`github.sha`). `latest` n'est qu'un alias. Un déploiement futur doit citer le SHA, ou le digest du registre une fois l'image réellement poussée. Le label `org.opencontainers.image.revision` porte le même SHA. Aucune image n'a été poussée par la préparation de ce gate.

Les quatre images d'une release viennent de la même révision. Ne pas associer une API d'un commit avec un worker ou une migration d'un autre.

Build local, sans publication :

```text
docker build --target grc-reminder-worker --tag medcore-grc-reminder-worker:<commit> .
```

`docker build .` sans `--target` reste l'API.

Une image de test locale n'est pas un artefact de release revu. B01 reste ouvert tant que le working tree n'est pas revu, commité, et que les images ne sont pas construites depuis ce commit.

## Processus et ordre

Ordre futur, avec un checkpoint humain après chaque étape critique :

```text
1. Décision de changement et fenêtre de maintenance
2. Sauvegarde vérifiée
3. Arrêt du worker de rappels GRC s'il tourne déjà
4. Job de migration unique
5. Vérification du schéma, sans écriture métier
6. Déploiement de l'API
7. Vérification de GET /health
8. Déploiement du frontend, avec PUBLIC_API_URL figé au build
9. Smokes en lecture seule
10. Démarrage du worker de rappels GRC
11. Lecture des logs du worker
12. Surveillance
```

Ne pas enchaîner automatiquement le déploiement parce que la migration a réussi.

Le worker de rendez-vous n'est pas le worker GRC. Les redémarrer ou non est une décision séparée.

## Configuration

Les secrets viennent de l'environnement du processus ou d'un secret de conteneur. Aucun n'est copié dans l'image. Ne pas committer de valeur réelle.

| Variable | Requis | Défaut | Secret | Processus |
|---|---|---|---|---|
| `APP_ENV` | non | `development` | non | API, migrate, workers |
| `PORT` | non | `8080` | non | API |
| `DATABASE_URL` | oui | aucun | oui | API, migrate, workers |
| `JWT_SECRET` | oui en production | `change_me` refusé si `APP_ENV=production` | oui | API |
| `CORS_ORIGIN` | oui en production, une origine exacte | `http://localhost:5173` | non | API |
| `MEDCORE_BUSINESS_TIMEZONE` | non | `UTC` | non | API, migrate, workers |
| `MEDCORE_TIMEZONE` | non | `UTC` | non | API |
| `PUBLIC_API_URL` | oui au build frontend | exemple local dans `.env.example` | non | frontend |
| `GRC_REMINDER_INTERVAL` | non | `1h`, doit être > 0 | non | worker GRC |
| `GRC_REMINDER_LEASE` | non | `15m`, doit être > 0 | non | worker GRC |
| `GRC_REMINDER_EVIDENCE_WINDOW_DAYS` | non | `30`, entier 1..365 | non | worker GRC |
| `GRC_REMINDER_ACTION_WINDOW_DAYS` | non | `30`, entier 1..365 | non | worker GRC |
| `GRC_REMINDER_POLICY_WINDOW_DAYS` | non | `30`, entier 1..365 | non | worker GRC |
| `MEDCORE_M365_TENANT_ID` | oui pour le worker GRC | aucun | non | worker GRC |
| `MEDCORE_M365_CLIENT_ID` | oui pour le worker GRC | aucun | non | worker GRC |
| `MEDCORE_M365_CLIENT_SECRET` | oui pour le worker GRC | aucun | oui | worker GRC |
| `MEDCORE_M365_SENDER` | oui pour le worker GRC | aucun | non | worker GRC |

Une durée invalide, une fenêtre hors 1..365, ou Microsoft 365 incomplet arrêtent le worker avant tout envoi. Le lot de claim est 25 et les tentatives maximales sont 3. Ces deux valeurs ne sont pas des variables.

`DATABASE_URL` vide ou illisible arrête le processus sans journaliser l'URL. En `APP_ENV=production`, `JWT_SECRET` vide ou égal à `change_me`, et `CORS_ORIGIN` vide ou `*`, arrêtent l'API.

Le seed de démonstration n'est pas lancé par l'API. Il exige `MEDCORE_ALLOW_DEMO_SEED=1` et `MEDCORE_DEMO_SEED_PASSWORD`. Il refuse `APP_ENV=production`, sauf l'exception pré-V1 explicite `MEDCORE_ALLOW_PRE_V1_DEMO_SEED=1` limitée à la base nommée `medcore`. Ne pas poser ces variables pour une mise en production GRC.

## Migration

`cmd/migrate` est le seul propriétaire du schéma. Il appelle `AutoMigrate` puis les contraintes et index déjà codés. Les tables GRC, y compris `grc_reminder_intents`, sont des tables nouvelles. Aucun backfill GRC n'est requis.

Politique : une seule exécution, avant la nouvelle API et avant le worker. Le mécanisme n'a pas de verrou consultatif. Deux migrateurs simultanés ne sont pas un mode supporté.

GORM `AutoMigrate` est prévu pour être relançable. Les tests de migration existants vérifient une seconde exécution sur une base de test. Ce n'est pas une migration réversible : il n'y a pas de script down.

Les index sont créés par le mécanisme GORM / SQL existant, pas par `CREATE INDEX CONCURRENTLY`. Sur une table déjà grande, une création d'index peut prendre un verrou. Les volumes de production sont inconnus. Les tables GRC nouvelles ne verrouillent pas les tables cliniques existantes au-delà des clés étrangères qu'elles référencent.

Ne pas lancer cette commande contre une base de production depuis ce jalon. Le jour venu, la cible doit être la base choisie pour cette release, jamais une base copiée ici par erreur :

```text
# Base cible à confirmer par l'opérateur. Ne pas coller une URL de production dans ce fichier.
APP_ENV=production DATABASE_URL=<DATABASE_URL_DE_LA_CIBLE> go run ./cmd/migrate
```

Le processus s'arrête avec un code d'erreur si la migration échoue. Ne pas la relancer en boucle. Décider entre correction avant et restauration.

Le nouveau code GRC n'est pas prouvé compatible avec l'ancien schéma. Ne pas faire de rolling update qui démarre l'API ou le worker avant la fin de la migration.

## Sauvegarde et restauration

`pg_restore` avec un code de sortie 0 ne suffit pas. Une reprise est réussie seulement si la restauration est propre, le schéma, les contraintes et les index critiques sont présents, les comptes de lignes attendus correspondent, et un smoke applicatif passe sur la base restaurée.

Aucune sauvegarde de production n'a été prise. Aucune base de production n'a été lue ni restaurée.

Répétition exécutée sur ce serveur local, données de test seulement :

```text
serveur PostgreSQL : 18.3 (Homebrew)
pg_dump : PostgreSQL 18.3 (Homebrew)
pg_restore : PostgreSQL 18.3 (Homebrew)
source : medcore_b03_source_<suffix>, créée vide pour la répétition
cible : medcore_restore_test_<suffix>, autre base, vide avant restauration
schémas : 1
tables public : 145
tables GRC et audit_logs : 20
contraintes GRC et audit : 305
index GRC et audit : 163
audit_logs : 16
grc_reminder_intents : 1
format : custom, sans owner, sans privilèges
taille : 751232 octets
SHA-256 : d965ce61e6e29cf6be726da9c15ccdf148b25c9fada9d14a862f53e1d03811de
durée dump / restore / smoke : 72 ms / 262 ms / 16 ms
```

Ces durées décrivent cette base de test presque vide. Ce ne sont pas un RPO, un RTO, ni un engagement de production.

```text
RPO = NOT DEFINED
RTO = NOT DEFINED
rétention des sauvegardes de production = décision d'infrastructure, non définie ici
```

`medcore_test` n'a pas été vidée ni remplie : 0 table dans `public` avant et après. `medcore` et `medcore_his` sont refusés par le garde-fou. Le test ne lit pas `DATABASE_URL` du fichier `.env`.

La répétition est `TestGRC15B03BackupRestoreRehearsal`, lancée seulement avec `MEDCORE_BACKUP_REHEARSAL=1`. Elle migre la source avec `cmd/migrate`, pose des lignes GRC de test, dump, vérifie le checksum, restaure dans la base vide, compare le manifeste, exécute les lectures GRC, une mutation avec `audit_logs`, un doublon refusé, un `ON DELETE RESTRICT`, un CHECK de rappel à deux cibles, puis relance `cmd/migrate` et une seconde mutation. Une copie tronquée du dump est refusée par `pg_restore`. Les bases et fichiers temporaires sont supprimés à la fin. Les noms autorisés sont `medcore_b03_source_<chiffres>`, `medcore_restore_test_<chiffres>` et `medcore_restore_corrupt_<chiffres>`.

PostgreSQL peut réécrire l'affichage d'un `CHECK` sur un tableau (`ARRAY['A']` ou `ARRAY[('A')::text]`). Les noms, types et actions de contraintes sont identiques. Les probes UNIQUE, CHECK et RESTRICT confirment qu'elles s'appliquent.

Procédure à suivre plus tard, sur une base dont le nom a été prouvé hors production :

```text
identifier current_database(), current_user et la version
refuser medcore et medcore_his
pg_dump --format=custom --no-owner --no-privileges
calculer SHA-256 et conserver taille et checksum hors du dump
pg_restore --list
revérifier le checksum avant restauration
créer une base vide de nom différent
pg_restore --no-owner --no-privileges --exit-on-error --single-transaction
comparer tables, contraintes, index et comptes de lignes
y compris audit_logs et grc_reminder_intents
smoke applicatif, dont une mutation GRC et son AuditLog
cmd/migrate doit être idempotent, puis refaire un smoke court
```

Ne pas lancer `cmd/migrate` avant cette comparaison : le dump doit déjà contenir le schéma. Ne pas réparer à la main une table, une séquence ou une clé étrangère. Un dump corrompu ou un checksum différent arrête la procédure.

Les sauvegardes de production devront être protégées par le contrôle d'accès, le chiffrement au repos, un transport protégé et une rétention décidée hors de ce dépôt. Ce rehearsal ne construit pas ce chiffrement. Il ne purge ni `audit_logs` ni `grc_reminder_intents`.

Avant une migration future de la base cible réelle :

```text
sauvegarder cette base
vérifier checksum, liste et restauration sur une base isolée
ne pas commencer la migration tant que cette vérification n'est pas faite
```

Retour arrière applicatif : redéployer l'image précédente par son tag SHA ou, une fois publié, par son digest de registre. Ne pas choisir seulement `latest`. Arrêter d'abord le worker GRC. L'API, le worker, la migration et le worker de notifications repris doivent être la même révision. Le frontend doit être reconstruit si `PUBLIC_API_URL` change.

Retour arrière base : pas de down migration. Choix réel :

```text
correction en avant si le schéma nouveau reste compatible
ou restauration de la sauvegarde vérifiée, puis redémarrage de l'artefact compatible
```

Restaurer par-dessus la base de production seulement après décision explicite. Ne pas rejouer une migration destructive pour « réparer ».

## Worker GRC

Image : `ghcr.io/lallene/medcore-his-grc-reminder-worker`. Cible Docker : `grc-reminder-worker`. Binaire : `./medcore-grc-reminder-worker`, compilé depuis `./cmd/grc-reminder-worker`. Commande d'image, forme exec : `["./medcore-grc-reminder-worker"]`. Pas de shell intermédiaire, donc `SIGINT` et `SIGTERM` arrivent au processus Go.

Base runtime : `alpine:latest`, utilisateur root, comme les autres images backend. Pas de healthcheck Docker : ce processus n'a pas de port HTTP. Ne pas lui en ajouter pour le packaging.

Variables au runtime seulement, jamais dans l'image : `DATABASE_URL`, `MEDCORE_M365_TENANT_ID`, `MEDCORE_M365_CLIENT_ID`, `MEDCORE_M365_CLIENT_SECRET`, `MEDCORE_M365_SENDER`, et les `GRC_REMINDER_*` optionnelles. Sans la configuration Microsoft 365 obligatoire, le processus sort en code non nul avant toute connexion utile et sans imprimer de secret. `DATABASE_URL` n'est lue qu'après cette vérification.

Commande source : `cmd/grc-reminder-worker`.

Plusieurs instances peuvent tourner. Le claim `FOR UPDATE SKIP LOCKED` empêche deux workers de prendre le même intent. Ce n'est pas une livraison exactly-once.

Garantie externe : at-least-once. Si le processus s'arrête après l'acceptation du message et avant l'enregistrement `DELIVERED`, un passage ultérieur peut renvoyer, dans la limite de trois tentatives.

Avant l'envoi, la cible est revérifiée. Un rappel devenu faux n'est pas envoyé et passe en `FAILED` terminal (`rappel obsolète`).

Arrêt : `SIGINT` ou `SIGTERM` annule le contexte, arrête le ticker et sort. Une requête Graph déjà partie est annulée par ce contexte. Pour une maintenance, une panne Microsoft 365 ou un incident : arrêter ce processus. L'API peut continuer. Les intents restent en base. Aucun nouveau claim n'a lieu tant que le processus est arrêté.

## Microsoft 365

Le worker de rappels utilise le transport Graph déjà partagé avec les notifications de rendez-vous. Ce n'est pas du SMTP.

```text
authentification : OAuth2 client credentials, secret d'application
jeton : https://login.microsoftonline.com/{tenant}/oauth2/v2.0/token
scope : https://graph.microsoft.com/.default
envoi : POST https://graph.microsoft.com/v1.0/users/{sender}/sendMail
succès : HTTP 202
permission application attendue : Mail.Send
consentement administrateur : requis pour une permission d'application
```

Le dépôt ne peut pas prouver que cette permission est accordée dans le tenant. `Mail.Send` d'application peut viser toutes les boîtes tant qu'Exchange n'a pas restreint l'application. Cette restriction n'est pas dans le code. Elle doit être faite côté Microsoft si la production l'exige. Aucun certificat ni identité managée n'est implémenté. La rotation de secret n'est pas automatisée.

```text
Secret rotation procedure = NOT DEFINED
```

Quand une rotation sera décidée : installer le nouveau secret, mettre à jour la configuration du worker sans l'écrire dans Git, redémarrer le worker, révoquer l'ancien secret, puis faire le smoke contrôlé. Le client HTTP a un délai de 30 secondes. Le transport ne réessaie pas. Un 429 reste un échec transitoire ; le worker GRC n'applique pas le délai `Retry-After` et ne fait pas de backoff. Le prochain passage peut réessayer, dans la limite de trois tentatives. 401, 403 et 404 sont permanents. 500 et un délai dépassé avant l'envoi sont transitoires. Si la requête a déjà été écrite et que la réponse se perd, l'échec est ambigu et terminal : pas de nouvel essai automatique. Si Microsoft a accepté le message et que l'enregistrement `DELIVERED` n'aboutit pas, un essai ultérieur peut renvoyer. La garantie externe reste at-least-once, pas exactly-once.

L'expéditeur vient uniquement de `MEDCORE_M365_SENDER`. Le destinataire est le propriétaire de la preuve, l'assigné de l'action, ou le propriétaire de la politique. Il n'y a pas d'adresse de repli. Le profil et l'utilisateur sont revérifiés au moment de l'envoi, ainsi que l'état de la cible. Un rappel obsolète n'appelle pas Graph.

Les messages sont du texte brut, sans HTML et sans URL. Ils contiennent un sujet fixe, la référence, le titre, une date UTC, et pour une action l'état d'échéance. Le titre est remis sur une seule ligne. Les descriptions, secrets et données cliniques ne sont pas des paramètres du message.

L'API n'envoie pas de rappel GRC et n'a pas besoin de Microsoft 365 pour répondre. Une panne Graph n'arrête pas l'API. Si la base est indisponible, le worker ne peut pas claim un intent, donc il n'envoie pas. Le worker GRC exige les quatre variables Microsoft au démarrage. Il n'a pas le drapeau d'activation des rendez-vous. Une configuration incomplète arrête le processus avant tout envoi. Les logs de livraison portent une classe (`outcome`) et non le corps Graph, le jeton, ni le secret.

Métriques Microsoft 365 du worker GRC : absentes. Le worker de rendez-vous a Prometheus ; ce n'est pas réutilisé ici.

Ce jalon a vérifié le code avec un serveur HTTP local. Il n'a pas contacté Microsoft et n'a pas envoyé d'e-mail réel.

```text
[ ] Entra app registration exists
[ ] correct tenant
[ ] Mail.Send application permission granted
[ ] admin consent granted
[ ] sender mailbox exists
[ ] application authorized to use only that sender
[ ] credential valid and not expired
[ ] outbound HTTPS to login.microsoftonline.com and graph.microsoft.com allowed for the worker only
[ ] worker configuration injected securely
[ ] controlled non-production recipient approved for final smoke
```

Smoke futur, non exécuté ici : un destinataire approuvé, l'expéditeur approuvé, un seul rappel synthétique, une seule instance du worker, accepter la réponse 202, vérifier le message reçu, vérifier `DELIVERED`, lire les logs, puis retirer la donnée de test selon les règles du domaine. Cela ne promet pas exactly-once.

Les messages contiennent la référence, le titre et une date UTC. Ils ne contiennent pas de description clinique, de menace, de vulnérabilité, de référence externe, ni de secret.

## Observabilité

L'API écrit des logs structurés JSON quand `APP_ENV=production`, via `slog`. Le worker aussi, après `logger.Init`. Les logs de rappel indiquent le scan, le nombre créé, les claims, le succès et l'échec. Ils ne contiennent ni corps d'e-mail, ni secret, ni URL de base.

`GET /health` répond 200 si la base répond au ping, sinon 503. Ce n'est pas séparé en liveness et readiness. Le worker GRC n'a pas d'endpoint HTTP. Son état se lit dans les logs et dans le fait que le processus tourne.

Prometheus existe pour le worker de rendez-vous seulement. Le domaine GRC n'émet pas de métriques. Les logs suffisent à exploiter le worker tel qu'il est écrit. Ils ne remplacent pas une alerte externe.

À surveiller hors du dépôt :

```text
API injoignable ou /health en échec
migration en échec
base injoignable
worker GRC arrêté alors qu'il devrait tourner
échecs de livraison répétés
panne Microsoft 365
capacité disque ou base
expiration TLS
échec de sauvegarde
```

La rétention des logs applicatifs, du worker et de la plateforme est l'affaire de l'infrastructure. L'application les émet seulement. Elle ne les purge pas.

`audit_logs` et `grc_reminder_intents` ne sont pas purgés. Leur croissance est durable. Le détail du cycle de vie est dans la section Data Retention / Lifecycle — GRC15-B09. Aucune purge automatique n'est autorisée tant que les décisions qui y sont marquées `UNDECIDED` ne sont pas prises hors du dépôt.

L'horloge système doit être synchronisée par l'infrastructure. Les échéances, les baux et les horodatages d'audit en dépendent. Les clés d'occurrence des rappels sont en UTC.

## Data Retention / Lifecycle — GRC15-B09

Cette section décrit le comportement technique réel. Elle ne fixe aucune durée juridique. Aucune source du dépôt ne justifie un nombre d'années. `RGPD/legal retention periods = NOT ESTABLISHED BY REPOSITORY`. La conservation des dossiers médicaux et patients est hors de ce gate : `Clinical/medical record retention = OUT OF B09 SCOPE`.

```text
Implemented technical behavior:
  aucune purge automatique
  aucun job, aucune commande, aucune route de rétention
  les registres GRC n'ont pas de soft-delete
  les clés étrangères GRC sont ON DELETE RESTRICT
  audit_logs n'a pas de clé étrangère vers l'objet métier
  la suppression HTTP d'une entité GRC n'existe pas

Business decisions:
  UNDECIDED

Legal/regulatory decisions:
  UNDECIDED

Undecided retention periods:
  toutes les entités GRC, AuditLog et ReminderIntent

Automatic deletion status:
  DISABLED

Backup retention limitation:
  effacer une ligne de la base vivante ne l'efface pas des sauvegardes déjà prises
  la rétention des sauvegardes reste décidée avec la stratégie de sauvegarde
  B03 ne purge ni audit_logs ni grc_reminder_intents

Platform log responsibility:
  l'application n'écrit pas ces logs dans PostgreSQL
  leur durée appartient à l'hébergeur
```

```text
ANONYMIZATION = NOT IMPLEMENTED
ARCHIVE BACKEND = NOT IMPLEMENTED
LEGAL HOLD = NOT IMPLEMENTED
```

`ARCHIVED` sur un référentiel et `RETIRED` sur une politique ou un contrôle sont des états métier. Ils ne déplacent rien vers un archive et ils n'autorisent pas un DELETE. Un incident `CLOSED`, un risque `CLOSED` ou `ACCEPTED`, une preuve `REVOKED` ou expirée, un constat `CLOSED` ou `ACCEPTED` restent dans le registre. L'expiration d'une preuve est calculée à la lecture depuis `expires_at`. Elle ne change pas le statut et ne supprime pas la ligne.

Les seuls DELETE HTTP du domaine retirent un lien : exigence-contrôle, contrôle-risque, évaluation-preuve. Ils ne suppriment pas l'entité. Ce ne sont pas des purges de rétention.

`audit_logs` porte `deleted_at` parce qu'il réutilise `BaseEntity`. Aucun chemin GRC ne le renseigne. Une future suppression GORM serait un soft-delete, pas une disparition physique, et elle masquerait la ligne des lectures ordinaires. B02 reste intact : la mutation métier et la ligne d'audit sont la même transaction. `record_id` n'est pas une clé étrangère. Supprimer un objet, si une décision future l'autorisait, ne doit jamais cascader vers l'audit. Les snapshots sont des allowlists. Ils peuvent contenir des identifiants d'acteur, de profil et des titres. Ils ne sont pas un motif pour inventer une durée.

`grc_reminder_intents` est une file technique : `PENDING`, `PROCESSING`, `DELIVERED`, `FAILED`. Il n'y a pas d'état obsolète stocké : un rappel devenu faux passe en `FAILED` avec `rappel obsolète`. La ligne garde l'identifiant du profil destinataire, la cible, la clé d'occurrence et une classe d'erreur fixe. Elle ne garde ni le corps du message, ni l'adresse, ni un secret. La contrainte unique `(recipient_profile_id, occurrence_key)` empêche un second envoi. Elle est la seule mémoire de cet envoi. Les clés étrangères vers la preuve, l'action, la politique et le profil sont `RESTRICT` : la cible ne peut pas disparaître tant que le rappel existe. Aucune durée technique n'est configurée. La purge automatique reste désactivée.

Une ligne `DELIVERED` n'est donc pas un simple résidu. Une action ouverte et en retard reste candidate à chaque scan, avec la même clé `action-overdue:<id>:<échéance>`. Supprimer son intent livré fait renvoyer le même e-mail au scan suivant. C'est prouvé sur PostgreSQL. À l'inverse, une preuve ou une politique dont l'échéance est passée n'est plus détectée : son intent supprimé ne revient pas. Une purge technique future devrait donc tenir compte de la cible, pas seulement de l'âge et du statut de la ligne. Elle ne serait sûre qu'une fois l'occurrence impossible à redétecter : action `DONE` ou `CANCELLED`, échéance modifiée, ou date de preuve ou de politique passée. Une action terminée peut être rouverte, donc même ce critère demande une décision. B09 n'implémente pas cette purge. Le volume reste borné : une ligne par destinataire et par occurrence d'échéance, sans boucle de retry qui ajoute des lignes.

Aucun champ de gel juridique n'existe. `INVESTIGATING` sur un incident et `IN_PROGRESS` sur un audit sont des états de travail, pas un legal hold. Une purge future ne doit pas partir avant qu'un gel explicite, ou une décision écrite qu'il n'est pas requis, existe.

```text
ENTITY | CLASS | TERMINAL STATE | AUTO DELETE | RETENTION PERIOD | AUTHORITY | NOTES
Asset | F | aucun imposé (RETIRED modifiable) | NO | UNDECIDED | UNDECIDED | RESTRICT vers risques, contrôles, incidents
Risk | B | aucun imposé (CLOSED, ACCEPTED modifiables) | NO | UNDECIDED | UNDECIDED | fermé ne veut pas dire supprimé
Framework | F | aucun imposé (ARCHIVED modifiable) | NO | UNDECIDED | UNDECIDED | état métier, pas un stockage d'archive
Requirement | F | aucun imposé (INACTIVE modifiable) | NO | UNDECIDED | UNDECIDED | parent et liens RESTRICT
Control | F | aucun imposé (RETIRED, INACTIVE modifiables) | NO | UNDECIDED | UNDECIDED | liens RESTRICT
ControlImplementation | B | aucun imposé | NO | UNDECIDED | UNDECIDED | preuves et actions l'empêchent de disparaître
Evidence | B | REVOKED (imposé) | NO | UNDECIDED | UNDECIDED | expires_at n'est pas une suppression ; métadonnées seulement
Assessment | B | COMPLETED, CANCELLED (imposés) | NO | UNDECIDED | UNDECIDED | lien de preuve détachable, entité conservée
AuditEngagement | D | COMPLETED, CANCELLED (imposés) | NO | UNDECIDED | UNDECIDED | constats RESTRICT
Finding | B | CLOSED, ACCEPTED (imposés) | NO | UNDECIDED | UNDECIDED | ne pas le supprimer si l'audit reste
RemediationAction | B | aucun imposé (DONE, CANCELLED réouvrables) | NO | UNDECIDED | UNDECIDED | RESTRICT vers constat, risque, contrôle
GovernancePolicy | A | RETIRED (imposé) | NO | UNDECIDED | UNDECIDED | RETIRED n'est pas DELETE
SecurityIncident | C | CLOSED (imposé) | NO | UNDECIDED | UNDECIDED | closed_at existe ; pas de purge
Liens (exigence-contrôle, contrôle-risque, évaluation-preuve, politique-référentiel, politique-contrôle) | F | n/a | NO | UNDECIDED | UNDECIDED | retrait métier audité, pas une purge
AuditLog | D | append-only | NO | UNDECIDED | UNDECIDED | pas de CASCADE ; soft-delete non utilisé par GRC
ReminderIntent | E | DELIVERED, FAILED | NO | UNDECIDED | UNDECIDED | clé d'unicité d'envoi ; une purge peut renvoyer un e-mail
```

`imposé` veut dire qu'une machine d'états refuse toute sortie de cet état. `aucun imposé` veut dire que le statut reste modifiable par une mise à jour autorisée. Un âge ou un statut seul ne suffit donc pas à rendre ces lignes éligibles à une purge.

Minimisation constatée : l'audit GRC ne renseigne ni `ip` ni `agent` dans `audit_logs`, même si ces colonnes existent. Les snapshots ne gardent que des champs allowlistés. `last_error` d'un rappel est une classe fixe. Aucune table GRC ne stocke de session ni de jeton : le JWT n'est pas persisté. Les comptes `users` portent un soft-delete. Un profil `staff_profiles` encore référencé par GRC ne peut pas être supprimé physiquement.

Niveau retenu pour B09 : `A — documentation/policy matrix only`, avec des tests PostgreSQL qui fixent les faits ci-dessus. Aucune durée n'étant décidée, un moteur générique n'aurait rien à appliquer. La seule donnée technique candidate, `grc_reminder_intents`, n'est pas éphémère au sens sûr (voir plus haut). Aucune configuration de rétention n'est ajoutée, aucune route, aucun worker.

Preuves PostgreSQL, sur une base ou un schéma temporaire uniquement :

```text
cmd/migrate  TestGRC15B09_SchemaKeepsGRCHistory
  toute clé étrangère qui touche une table grc_* est ON DELETE RESTRICT
  audit_logs n'a aucune clé étrangère
  aucune table grc_* n'a de colonne deleted_at

internal/modules/grc  TestPostgresGRC15B09RetentionLifecycle
  une ligne métier supprimée en SQL brut laisse sa ligne audit_logs intacte
  un intent de rappel bloque la suppression de sa preuve (RESTRICT)
  supprimer l'intent livré d'une action en retard recrée le même rappel PENDING
  supprimer l'intent d'une preuve expirée ne le recrée pas
```

Décisions encore à obtenir, aucune n'est acquise :

```text
Security / DPO — durée et contenu acceptable d'AuditLog et des identifiants de profil
Legal — durées réglementaires, y compris les incidents
Compliance — durée de valeur des preuves, audits, constats et actions
Hospital governance — politiques retirées, risques acceptés, actifs retirés
Operations — croissance de grc_reminder_intents et rétention des sauvegardes et des logs de plateforme
```

Tant que ces décisions manquent, le comportement sûr est `NO AUTOMATIC DELETION`.

## Audit

Une mutation GRC qui répond avec succès écrit sa ligne `audit_logs` dans la même transaction PostgreSQL que l'écriture métier. Si l'insertion d'audit échoue, la mutation est annulée et l'API ne répond pas succès. Il n'y a pas de second événement `audit.event` pour ces mutations : `MemoryBus` ne crée donc pas un doublon.

```text
modèle GRC : transactionnel, même base
mutation et AuditLog atomiques : oui
événement GRC pouvant être perdu après un succès : non
```

Les autres domaines qui publient encore sur `MemoryBus` restent best-effort. Un handler de ce bus qui panique est rattrapé et journalisé. Le processus reste vivant. Cela ne concerne pas le chemin de succès de l'audit GRC.

Les rappels GRC n'écrivent pas d'audit métier. Leur état est la table `grc_reminder_intents`. Aucun acteur humain n'est inventé pour un scan.

Les snapshots restent les allowlists déjà validées. L'échec d'audit est journalisé sans le snapshot et sans le SQL.

## Sécurité hors code

CORS : une origine explicite, comparée exactement à l'en-tête `Origin`. Les credentials ne sont autorisés que pour cette origine. `*` est refusé en production.

`SetTrustedProxies(nil)` : Gin ne fait pas confiance à `X-Forwarded-For`, `X-Forwarded-Proto` ni `X-Forwarded-Host`. L'adresse client vue par l'API est celle du pair TCP direct. Aucune redirection HTTPS ne dépend de ces en-têtes.

JWT : HMAC HS256, expiration 24 heures, en-tête `Authorization: Bearer`. Le jeton est stocké par le frontend dans `localStorage`, pas dans un cookie. Authentication cookies = NOT USED. CORS n'est pas une protection CSRF : le navigateur n'attache pas ce jeton tout seul.

## TLS et en-têtes — GRC15-B05

Preuve dépôt, architecture cible documentée et preuve de production sont trois choses distinctes. Cette section décrit les deux premières. Elle ne prouve pas le DNS, le certificat ni les ports réellement publiés.

```text
Internet
   |
 HTTPS :443
   |
[plateforme TLS — hors dépôt]
   |\
   | \--> frontend HTTP privé, sirv via frontend/server.mjs
   |
   +----> API Go HTTP privé, gin.Router.Run
```

Le dépôt ne contient pas de Caddy, Nginx, Traefik, Apache, Cloudflare ni compose. Les Dockerfiles écoutent en HTTP : l'API sur `:$PORT` (défaut 8080, toutes interfaces) et le frontend sur `0.0.0.0:$PORT` (défaut 10000). Le seul hôte public nommé par la CI et le swagger est `medcore-his-api-latest.onrender.com`. Ce nom n'est pas une preuve que Render termine TLS aujourd'hui, ni de son mode TLS, ni de son certificat.

```text
API PORT MUST NOT BE PUBLICLY EXPOSED
FRONTEND PORT MUST NOT BE PUBLICLY EXPOSED
443 public, porté par la plateforme
80 public seulement s'il redirige vers HTTPS, à la plateforme
```

Le processus Go n'appelle pas `ListenAndServeTLS`. Le frontend non plus. Le trafic public doit être HTTPS. Le login, le JWT et les données métier ne doivent pas passer en HTTP public. La redirection HTTP vers HTTPS appartient à la plateforme, pas à l'application : l'application ne lit pas `X-Forwarded-Proto`.

TLS minimum attendu à la plateforme : TLS 1.2. TLS 1.3 accepté et préféré si la plateforme le permet. Le dépôt ne peut pas l'imposer.

Certificat : mécanisme plateforme, non prouvé. Production certificate renewal = EXTERNAL VERIFICATION REQUIRED. Surveillance et alerte d'expiration appartiennent à la plateforme. Aucun certificat n'est demandé ni stocké ici.

HSTS, une seule valeur, sans `includeSubDomains` et sans `preload` :

```text
Strict-Transport-Security: max-age=31536000
```

L'API et le serveur statique l'émettent. Un navigateur l'ignore tant que la réponse n'est pas reçue en HTTPS. La plateforme ne doit pas ajouter une seconde valeur différente. Cloudflare n'est pas configuré dans le dépôt ; s'il est placé devant plus tard, sa vérification reste externe et son mode ne doit pas accepter un certificat d'origine invalide.

Propriétaires :

```text
TLS, redirection HTTP, ports publics, renouvellement : PLATFORM
HSTS, nosniff, frame, referrer, permissions : GO API pour l'API, FRONTEND SERVER pour le HTML
CSP HTML : FRONTEND SERVER
CSP JSON : GO API
CORS : GO API
Cache des assets hashés : FRONTEND SERVER, non désactivé
Cache-Control no-store : GO API sur /, /health et /api
Cache-Control no-cache : FRONTEND SERVER sur les documents HTML
```

Matrice :

```text
HEADER | HTML | STATIC | API | OWNER | VALUE
Strict-Transport-Security | yes | yes | yes | FRONTEND SERVER et GO API, même valeur | max-age=31536000
Content-Security-Policy | yes | yes | yes | FRONTEND SERVER ou GO API | voir ci-dessous
X-Content-Type-Options | yes | yes | yes | FRONTEND SERVER et GO API | nosniff
X-Frame-Options | yes | yes | yes | FRONTEND SERVER et GO API | DENY
frame-ancestors | yes | yes | yes | même réponse que la CSP | 'none'
Referrer-Policy | yes | yes | yes | FRONTEND SERVER et GO API | strict-origin-when-cross-origin
Permissions-Policy | yes | yes | yes | FRONTEND SERVER et GO API | camera, microphone, geolocation, clipboard, fullscreen, payment désactivés
Cache-Control | no-cache | inchangé | no-store sur /api /health / | voir propriétaires | —
```

CSP frontend, construite depuis `PUBLIC_API_URL` au démarrage du serveur statique. Cette variable doit être la même qu'au build. Aucun domaine n'est écrit en dur.

```text
default-src 'self'
script-src 'self' 'sha256-<script inline de build/index.html>'
style-src 'self' 'unsafe-inline'
img-src 'self' blob: data: <origine de PUBLIC_API_URL si elle est http(s)>
font-src 'self'
connect-src 'self' <même origine>
object-src 'none'
base-uri 'self'
frame-ancestors 'none'
form-action 'self'
```

`unsafe-inline` est limité aux styles. `app.html` contient `style="display: contents"` et plusieurs composants posent des attributs `style` pour les graphiques. Le build SvelteKit place un script inline dans `build/index.html` pour démarrer l'application. Le serveur statique en calcule le hash SHA-256 au démarrage et l'ajoute à `script-src`. Il n'ajoute pas `unsafe-inline` ni `unsafe-eval` aux scripts. Aucun CDN, WebSocket ni SSE n'a été trouvé. `blob:` et `data:` couvrent les aperçus PDF et les images locales. Les photos patient peuvent venir de l'origine API.

CSP API, sauf `/swagger` :

```text
default-src 'none'; base-uri 'none'; object-src 'none'; frame-ancestors 'none'; form-action 'none'
```

`/swagger` ne reçoit pas `default-src 'none'`, sinon l'interface ne charge plus ses scripts. Elle garde `frame-ancestors 'none'`, `object-src 'none'`, `base-uri 'none'` et `form-action 'none'`.

Aucune iframe n'est exigée. L'embarquement est interdit.

`/health` reste public. Le corps contient le statut, le nom `medcore-his-api` et `db` up ou down, ou `draining` pendant l'arrêt. Pas de secret, pas de chemin, pas de nom d'hôte interne.

Les erreurs GRC non typées répondent `Erreur interne`. Elles ne renvoient pas le SQL.

Limites de corps, timeouts HTTP, arrêt propre et pool SQL sont traités par GRC15-B07 (section API Edge / HTTP Hardening). Le rate limiting par IP reste à la charge de l'edge.

Vérification de production, à exécuter seulement quand elle sera autorisée. Ne pas la lancer depuis ce dépôt :

```text
[ ] DNS resolves to intended edge
[ ] HTTPS certificate valid
[ ] hostname matches certificate
[ ] certificate not near expiry
[ ] TLS 1.0/1.1 rejected
[ ] TLS 1.2/1.3 accepted as intended
[ ] HTTP redirects to HTTPS
[ ] HSTS present over HTTPS
[ ] CSP present on HTML
[ ] nosniff present
[ ] frame protection present
[ ] Referrer-Policy present
[ ] Permissions-Policy present
[ ] CORS only expected origins
[ ] API internal port not publicly reachable
[ ] forwarded headers originate only from trusted edge
[ ] health endpoint exposes no sensitive information
```

Retour arrière : retirer le middleware Go et revenir au `sirv-cli` précédent n'enlève pas un certificat. Ne pas changer le DNS ni le pare-feu pour annuler ce gate.

Ces points, relevés par GRC15-B05 (absence de timeouts HTTP, de limite de corps, de maximum du pool SQL et d'arrêt sur `SIGTERM`), sont traités côté code par GRC15-B07. Voir la section suivante.

## API Edge / HTTP Hardening — GRC15-B07

Périmètre : code, configuration, tests et documentation de l'API. Aucun changement d'edge, de Render, de Cloudflare, de DNS, de pare-feu ni de base de production. Les contrôles de l'edge restent à vérifier hors dépôt.

### Architecture

```text
client ──HTTPS──▶ edge (TLS, WAF, rate limit IP, timeouts proxy)
                   │ HTTP privé
                   ▼
            medcore-api (Go net/http + Gin, :$PORT)
                   │ pool database/sql borné
                   ▼
              PostgreSQL
```

L'API écoute en HTTP clair sur `:$PORT`. TLS, HTTP→HTTPS et HSTS en transport appartiennent à l'edge (GRC15-B05). Le port interne ne doit pas être joignable depuis Internet.

### Serveur HTTP

`application.Run` construit un `http.Server` explicite. `Router.Run` n'est plus utilisé.

| Réglage | Défaut | Effet |
|---|---|---|
| `ReadHeaderTimeout` | 10s | Coupe un client lent sur les en-têtes (slowloris). |
| `ReadTimeout` | 2m | Borne la lecture complète de la requête, corps compris. |
| `WriteTimeout` | 2m | Borne le traitement et l'écriture de la réponse. Aucune route de streaming, SSE ni WebSocket. |
| `IdleTimeout` | 2m | Ferme les connexions keep-alive inactives. |
| `MaxHeaderBytes` | 64 KiB | Au-delà, `net/http` répond 431. |

Le timeout d'idle de l'edge doit être inférieur à `IdleTimeout` de l'API, et le timeout de requête de l'edge cohérent avec `WriteTimeout`.

### Limites de corps

Middleware global `BodyLimit`, placé après CORS et les en-têtes de sécurité, avant les routes :

- JSON et autres corps : `MEDCORE_HTTP_MAX_BODY_BYTES` (1 MiB).
- `multipart/form-data` : `MEDCORE_HTTP_MAX_UPLOAD_BYTES` (8 MiB). Seule route concernée : `POST /api/tickets/:id/attachments`, qui garde sa propre limite de 5 MiB + 1 KiB et son contrat d'erreur.
- `Content-Length` déclaré au-dessus de la limite : 413 `PAYLOAD_TOO_LARGE` avant le handler, avec `Connection: close`.
- Corps chunké ou sans `Content-Length` : la limite s'applique pendant la lecture (`http.MaxBytesReader`). Au dépassement, la réponse devient 413 `PAYLOAD_TOO_LARGE`, quel que soit le code choisi par le handler.
- Une requête exactement à la limite est acceptée.

Les documents médicaux et les preuves GRC sont des références (URL ou texte), pas des fichiers : aucun autre upload.

### Arrêt propre

`SIGINT` ou `SIGTERM` :

1. `/health` répond 503 `{"status":"draining"}` : l'edge retire l'instance.
2. `http.Server.Shutdown` ferme l'écoute et laisse finir les requêtes en cours.
3. Après `MEDCORE_HTTP_SHUTDOWN_TIMEOUT` (25s), les connexions restantes sont fermées de force et le processus sort en code 1.
4. Le pool SQL est fermé.

Le délai de la plateforme avant `SIGKILL` doit être supérieur à `MEDCORE_HTTP_SHUTDOWN_TIMEOUT`. L'image lance le binaire en forme exec, sans shell : le signal arrive au processus Go.

### Pool SQL

Appliqué dans `database.Connect`, donc à l'API, aux workers, à `migrate`, `seed` et `qa-import`.

| Variable | Défaut | Bornes |
|---|---|---|
| `MEDCORE_DB_MAX_OPEN_CONNS` | 10 | 1 à 200 |
| `MEDCORE_DB_MAX_IDLE_CONNS` | 5 (abaissé à max open si plus petit) | 0 à 200, ≤ max open |
| `MEDCORE_DB_CONN_MAX_LIFETIME` | 30m | 1m à 24h |
| `MEDCORE_DB_CONN_MAX_IDLE_TIME` | 5m | 30s à 24h, ≤ lifetime |

À saturation, `database/sql` fait attendre la requête jusqu'à la fin de son contexte ou de `WriteTimeout`. Il n'y a pas de file applicative. Peu d'appels transmettent aujourd'hui le contexte de requête à GORM : une requête abandonnée par le client peut garder sa connexion jusqu'à la fin de la requête SQL. Écart connu, non refactoré dans ce gate.

Budget de connexions, à vérifier contre `max_connections` de la base de production (NON VÉRIFIÉ) :

```text
répliques API × MEDCORE_DB_MAX_OPEN_CONNS
+ notification-worker × MEDCORE_DB_MAX_OPEN_CONNS
+ grc-reminder-worker × MEDCORE_DB_MAX_OPEN_CONNS
+ réserve migration / administration / supervision
< max_connections du serveur (ou limite du pooler)
```

Les workers lisent les mêmes variables. Les régler par processus si leur besoin diffère.

### Rate limiting

Go voit seulement l'adresse de l'edge : `SetTrustedProxies(nil)` ignore `X-Forwarded-For`. Un limiteur par IP dans Go limiterait l'edge entier. Aucun limiteur n'est ajouté dans Go, et aucun 429 n'est émis par l'API.

- Rate limit par IP, dont `POST /api/auth/login` : à configurer sur l'edge. EXTERNAL REQUIRED.
- Rate limit par identité authentifiée dans Go : non, pas de besoin démontré. Un limiteur en mémoire ne tiendrait pas avec plusieurs répliques.

### Proxy de confiance et IP client

`SetTrustedProxies(nil)` est conservé. Aucun CIDR d'edge vérifié n'existe dans le dépôt. Faire confiance à tous les proxies est interdit. `c.ClientIP()` vaut le pair TCP, donc l'edge. Pour obtenir l'IP réelle, il faudra un CIDR d'edge vérifié et un gate dédié.

### Santé

`GET /health` sert de readiness :

| État | Code | Corps |
|---|---|---|
| prêt | 200 | `{"status":"ok","service":"medcore-his-api","db":"up"}` |
| base injoignable (ping borné à 2s) | 503 | `{"status":"error","db":"down"}` |
| arrêt en cours | 503 | `{"status":"draining"}` |

Pas de secret, pas de DSN, pas d'erreur SQL. Le notification-worker garde `/healthz` et `/readyz` sur son propre port.

### Surcharge

| Situation | Réponse |
|---|---|
| Corps trop gros | 413 `PAYLOAD_TOO_LARGE` |
| En-têtes trop gros | 431 (`net/http`) |
| Client lent | connexion fermée par les timeouts |
| Pool SQL saturé | attente, bornée par `WriteTimeout` côté client |
| Base indisponible | 503 sur `/health` ; erreurs 500 génériques sur les routes |
| Arrêt | 503 `draining`, puis refus de connexion |
| Trop de requêtes par IP | 429 de l'edge, si configuré |

### Configuration

Toutes les valeurs sont des durées Go (`10s`, `2m`) ou des entiers en octets. Vide : défaut. Valeur explicite invalide ou hors bornes : refus de démarrer. Le message nomme la variable et les bornes, jamais la valeur.

| VARIABLE | DEFAULT | MIN / MAX | OWNER | PURPOSE |
|---|---|---|---|---|
| `MEDCORE_HTTP_READ_HEADER_TIMEOUT` | 10s | 1s / 1m | API | Lecture des en-têtes |
| `MEDCORE_HTTP_READ_TIMEOUT` | 2m | 5s / 10m, ≥ read header | API | Lecture de la requête |
| `MEDCORE_HTTP_WRITE_TIMEOUT` | 2m | 5s / 10m | API | Traitement et réponse |
| `MEDCORE_HTTP_IDLE_TIMEOUT` | 2m | 5s / 10m | API | Keep-alive inactif |
| `MEDCORE_HTTP_SHUTDOWN_TIMEOUT` | 25s | 1s / 2m | API | Délai de grâce à l'arrêt |
| `MEDCORE_HTTP_MAX_HEADER_BYTES` | 65536 | 8192 / 1048576 | API | Taille des en-têtes |
| `MEDCORE_HTTP_MAX_BODY_BYTES` | 1048576 | 65536 / 67108864 | API | Corps non multipart |
| `MEDCORE_HTTP_MAX_UPLOAD_BYTES` | 8388608 | 5243904 / 67108864, ≥ body | API | Corps multipart |
| `MEDCORE_DB_MAX_OPEN_CONNS` | 10 | 1 / 200 | API, workers, outils | Connexions ouvertes |
| `MEDCORE_DB_MAX_IDLE_CONNS` | 5 | 0 / 200, ≤ max open | API, workers, outils | Connexions au repos |
| `MEDCORE_DB_CONN_MAX_LIFETIME` | 30m | 1m / 24h | API, workers, outils | Recyclage des connexions |
| `MEDCORE_DB_CONN_MAX_IDLE_TIME` | 5m | 30s / 24h, ≤ lifetime | API, workers, outils | Fermeture au repos |

### Contrat de l'edge

À vérifier hors dépôt. Aucune de ces valeurs n'est configurée par ce gate.

```text
[ ] TLS terminé à l'edge, HTTP→HTTPS, HSTS (GRC15-B05)
[ ] Port interne de l'API non joignable depuis Internet
[ ] Edge supprime ou écrase X-Forwarded-For entrant
[ ] Rate limit par IP, renforcé sur POST /api/auth/login
[ ] Limite de corps edge ≥ 8 MiB sur /api/tickets/*/attachments, ≤ celle de l'API ailleurs si possible
[ ] Timeout de requête edge cohérent avec WRITE_TIMEOUT (2m)
[ ] Timeout idle edge < IDLE_TIMEOUT de l'API (2m)
[ ] Health check edge sur GET /health, 503 = retrait
[ ] Délai avant SIGKILL > SHUTDOWN_TIMEOUT (25s)
[ ] Somme des pools < max_connections de la base
```

### Checklist de production

```text
[ ] Variables MEDCORE_HTTP_* et MEDCORE_DB_* absentes ou dans leurs bornes
[ ] Démarrage API sans "configuration invalide"
[ ] Log de connexion indique max_open_conns attendu
[ ] GET /health = 200 db=up
[ ] SIGTERM : /health passe à 503 draining, puis sortie avant le délai de la plateforme
[ ] Corps JSON > 1 MiB → 413
[ ] Pièce jointe ticket ≤ 5 MiB acceptée
[ ] Rate limit edge vérifié sur /api/auth/login
[ ] Budget de connexions calculé contre max_connections
```

Retour arrière : remettre les variables à vide redonne les défauts. Retirer le gate demande un artefact précédent, pas un changement d'edge.

## Smokes futurs

Préconditions : artefact identifié, migration terminée, compte autorisé, aucun seed de démonstration.

Lecture seule :

```text
GET /health
authentification du compte prévu
GET /api/grc/dashboard
GET /api/grc/reporting
GET d'un registre autorisé
chargement du frontend
navigation GRC visible selon les permissions
```

Base, sans écriture métier :

```text
les tables GRC existent
grc_reminder_intents existe
les index et contraintes critiques annoncés par la migration existent
le nom de la base est celui de la cible
```

Worker, sans message réel :

```text
le processus démarre
le log de scan apparaît
le processus ne s'arrête pas tout seul
aucun corps d'e-mail dans les logs
```

Un rappel réel ou une mutation de registre n'est pas un smoke par défaut. S'il est autorisé plus tard, l'objet doit être un objet de test nommé, et les registres GRC n'ont pas de suppression.

## Incidents

Worker GRC en défaut : arrêter ce processus. Ne pas arrêter l'API si le problème est limité aux rappels.

Microsoft 365 en panne : les intents restent en base, les tentatives sont bornées, le worker peut être arrêté, l'API continue.

Migration en échec : ne pas démarrer la nouvelle API ni le worker. Conserver les logs. Ne pas relancer une migration destructive. Choisir correction avant ou restauration.

Base indisponible : arrêter les déploiements. L'API répond 503 sur `/health`. Ne pas inventer une réparation destructive.

## Checklist

PRE-DEPLOY

```text
[ ] Changement approuvé
[ ] Commit et artefact immuable identifiés
[ ] Revue effectuée
[ ] APP_ENV=production
[ ] JWT_SECRET présent et différent de change_me
[ ] CORS_ORIGIN est l'origine exacte du frontend
[ ] PUBLIC_API_URL de production figé au build frontend
[ ] Secrets Microsoft 365 présents seulement sur le worker
[ ] Variables de seed demo absentes
[ ] Sauvegarde terminée et vérifiée
[ ] Worker GRC arrêté
```

MIGRATION

```text
[ ] Une seule exécution de cmd/migrate
[ ] Code de sortie succès
[ ] Schéma GRC et grc_reminder_intents présents
[ ] Checkpoint humain avant de continuer
```

API

```text
[ ] Processus démarré après la migration
[ ] GET /health = 200 et db=up
[ ] Pool SQL et timeouts HTTP dans leurs bornes (GRC15-B07)
[ ] Aucun secret dans les logs
```

FRONTEND

```text
[ ] Build avec la PUBLIC_API_URL de la cible
[ ] Page servie en HTTPS par le proxy
[ ] L'API appelée n'est pas localhost
```

WORKER

```text
[ ] Démarré seulement après le schéma
[ ] Log de démarrage sans secret
[ ] Arrêt possible par SIGTERM sans arrêter l'API
```

SMOKE

```text
[ ] Smokes lecture seule réussis
[ ] Aucun seed lancé
[ ] Aucun e-mail réel non autorisé
```

MONITORING

```text
[ ] Logs API et worker collectés
[ ] Alerte base, API, worker et sauvegarde définie hors du dépôt
```

ROLLBACK

```text
[ ] Artefact précédent identifié
[ ] Sauvegarde restaurable sur une base isolée
[ ] Décision correction avant ou restauration écrite
```

SIGN-OFF

```text
[ ] Exploitation
[ ] Responsable applicatif
[ ] Les autres événements MemoryBus hors GRC restent best-effort : décision écrite si un autre domaine l'exige
```
