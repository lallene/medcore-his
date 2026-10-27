# Rappels GRC

Worker dédié. Il ne démarre pas dans le processus API.

Le worker détecte trois rappels, les enregistre une fois, puis les envoie par e-mail. Il ne change aucun statut métier : preuve, action, politique, risque, constat, incident, audit ou évaluation.

## Architecture

```text
état GRC (lecture)
  → détecteur
  → grc_reminder_intents
  → claim FOR UPDATE SKIP LOCKED
  → email.Transport (Microsoft 365)
  → statut de livraison
```

Commande : `cmd/grc-reminder-worker`.

Le transport est celui de `internal/shared/email/microsoft365`. Les intents de rendez-vous ne sont pas réutilisés.

Si Microsoft 365 n'est pas configuré, le processus s'arrête au démarrage. Aucun intent n'est alors marqué livré.

## Configuration

| Variable | Défaut | Règle |
|---|---|---|
| `GRC_REMINDER_INTERVAL` | `1h` | durée Go > 0 |
| `GRC_REMINDER_LEASE` | `15m` | durée Go > 0 |
| `GRC_REMINDER_EVIDENCE_WINDOW_DAYS` | `30` | entier 1..365 |
| `GRC_REMINDER_ACTION_WINDOW_DAYS` | `30` | entier 1..365 |
| `GRC_REMINDER_POLICY_WINDOW_DAYS` | `30` | entier 1..365 |
| `MEDCORE_M365_TENANT_ID` | — | requis |
| `MEDCORE_M365_CLIENT_ID` | — | requis |
| `MEDCORE_M365_CLIENT_SECRET` | — | requis, worker seulement |
| `MEDCORE_M365_SENDER` | — | requis |

`DATABASE_URL` et le fuseau métier suivent la configuration existante. Les dates des e-mails sont en UTC.

Le lot de claim est 25, la même taille par défaut que le worker de rendez-vous. Il n'y a pas de variable pour l'augmenter.

Les tentatives maximales sont 3. Ce n'est pas la limite du worker de rendez-vous. Il n'y a pas de backoff progressif : un échec transitoire redevient éligible au passage suivant. Un échec permanent, une issue ambiguë, un destinataire inactif ou une adresse inutilisable consomme le reste des tentatives et ne repart pas.

Un bail expiré (`lease_expires_at < now`) repasse en `FAILED` sans incrémenter une seconde fois le compteur déjà pris au claim. Un intent `DELIVERED` n'est plus envoyé.

## Rappels

| Kind | Condition | Destinataire |
|---|---|---|
| `EVIDENCE_EXPIRING` | validité calculée `EXPIRING_SOON` | `Evidence.OwnerProfileID` |
| `ACTION_DUE` | échéance calculée `DUE_SOON` ou `OVERDUE` | `RemediationAction.AssigneeProfileID` |
| `POLICY_EXPIRING` | `PUBLISHED` et `now <= effectiveTo <= now + fenêtre` | `GovernancePolicy.OwnerProfileID` |

Pas de destinataire de repli. Sans profil, ou si le profil est inactif, ou si l'utilisateur est inactif, supprimé ou sans adresse utilisable, aucun intent n'est créé. Les intents déjà enregistrés restent.

Les clés d'occurrence sont l'identifiant et l'instant UTC en nanosecondes :

```text
evidence-expiring:{id}:{unixNano}
action-due-soon:{id}:{unixNano}
action-overdue:{id}:{unixNano}
policy-expiring:{id}:{unixNano}
```

L'unicité est `(recipient_profile_id, occurrence_key)`. Un second scan ne crée pas un second intent. Une nouvelle date crée une nouvelle occurrence. `DUE_SOON` et `OVERDUE` sont deux occurrences.

Le canal est uniquement l'e-mail. Le message contient la référence, le titre et la date. Il ne contient pas de description, de référence externe, ni de donnée clinique. Il n'ajoute pas d'URL.

L'adresse est relue sur l'utilisateur au moment de l'envoi. Avant cet envoi, le worker revérifie la cible avec la même règle que le détecteur : la date de l'occurrence doit être inchangée, et l'état calculé doit encore être celui du rappel (`EXPIRING_SOON`, `DUE_SOON`, `OVERDUE`, ou politique `PUBLISHED` encore dans la fenêtre). Sinon aucun e-mail n'est envoyé et l'intent passe en `FAILED` terminal (`rappel obsolète`). Un intent déjà `DELIVERED` n'est pas réécrit.

Le claim est unique. L'envoi externe ne l'est pas : si le processus s'arrête après l'acceptation du message et avant l'enregistrement de `DELIVERED`, un passage ultérieur peut renvoyer, dans la limite des trois tentatives.

Aucun événement d'audit métier n'est publié : les objets GRC ne changent pas. L'historique de livraison est la table des intents et les logs du worker (`scan start`, nombre créés, claims, succès, échec). Pas de corps d'e-mail dans les logs. Pas de métrique ajoutée.
