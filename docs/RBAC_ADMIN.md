# LOT 21 — RBAC Administration

## Modèle

```
Utilisateur
  ├─ Fonction(s)  → permissions héritées (matrice Go + overlays DB)
  ├─ Spécialité(s) → pack médecin
  ├─ Service principal + secondaires → scope
  └─ Overrides GRANT / DENY → exceptions utilisateur
        ↓
Permissions effectives (auth middleware, chaque requête)
```

## Priorité de calcul

1. `role=admin` → `*` (wildcard) — **overrides ignorés**
2. Sinon héritage : base + fonctions (avec overlays matrice) + pack spécialité
3. Puis **DENY explicite** retire
4. Puis **GRANT explicite** ajoute
   (un DENY gagne toujours sur un GRANT pour la même clé)

Implémentation : `rbac.EffectiveStaffPermissionsFull` + `access.Service.ComputeEffectivePermissions`.

## Permissions ACC

| Clé | Usage |
|-----|--------|
| `rbac.read` | Lire le centre d'accès |
| `rbac.user.manage` | Activer / fonctions / services |
| `rbac.override.manage` | GRANT/DENY utilisateur |
| `rbac.matrix.manage` | Overlay matrice fonction×permission |
| `rbac.audit.read` | Historique RBAC |

Accordées à `DIRECTEUR_ADMINISTRATIF`. Les routes acceptent aussi `staff.read` / `staff.manage` / `staff.audit.read` pour compatibilité.

## API

- `GET /api/access/kpis`
- `GET /api/access/users`
- `GET /api/access/users/:id`
- `PUT /api/access/users/:id/active|functions|services`
- `POST/DELETE /api/access/users/:id/overrides[/:permission]`
- `GET /api/access/matrix` · `POST /api/access/matrix` (`rbac.matrix.manage`)
- `GET /api/access/permissions`
- `GET /api/access/users/:id/simulate` (read-only, pas de JWT)
- `GET /api/access/audit` · `.../users/:id/audit`

## Anti-lockout

Interdit de retirer le dernier compte capable d'administrer RBAC (`staff.manage` / `rbac.user.manage` / `admin`) via :

- ACC : désactivation, DENY, retrait de fonctions ;
- Staff Upsert (`PUT/POST /api/staff`) — même règle via `staff.AfterProfileChangeValidate` ;
- Overlay matrice : DENY/CLEAR qui laisserait zéro administrateur RBAC (rollback automatique).

## Comptes désactivés

`users.is_active` **et** `staff_profiles.active` doivent être alignés (ACC `SetActive`, Staff Upsert).
`ComputeEffectivePermissions` retourne `[]` si l’un des deux est inactif. Le middleware refuse aussi un profil staff inactif même si le flag user est encore true.

## Tables

- `staff_permission_overrides`
- `rbac_matrix_overrides`
- `rbac_access_audit_events`

La matrice Go `StaffFunctionPermissions` reste la source par défaut.
