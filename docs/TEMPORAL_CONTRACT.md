# MedCore HIS — temporal / timezone contract (LOT 26B)

## Roles

| Config | Role |
|---|---|
| **`MEDCORE_BUSINESS_TIMEZONE`** | Deployment-scoped IANA zone for the hospital **civil calendar** (“today”, overdue, SQL `CURRENT_DATE` alignment). Default: `UTC`. |
| **`MEDCORE_TIMEZONE`** | IANA zone for **scheduling** wall-clock / recurrence (`scheduling.Location`). Default: `UTC`. Independent of business timezone. |

Do **not** treat `MEDCORE_TIMEZONE` as the global business calendar without an explicit product decision.

## PostgreSQL

- **`DATE`** columns store civil **Y/M/D** only. Compare via civil-day extraction (`time.Time.Date()` / `businessdate.FromTime`). Never reinterpret a DATE into another zone before comparing days.
- **Event / audit / worker timestamps** are absolute instants (typically `timestamptz`). Compare as instants; format for humans with a presentation zone later.
- On connect, MedCore sets the PostgreSQL session **`TimeZone`** to `MEDCORE_BUSINESS_TIMEZONE` through **pgx `RuntimeParams`** on the shared connection config used by the `database/sql` pool (same approach as `gorm.io/driver/postgres`’s documented `TimeZone=` DSN handling). Every pooled connection therefore sees a matching `CURRENT_DATE`.

A one-shot `SET TIME ZONE` on a single borrowed connection is **not** sufficient for a pool.

## Forbidden pattern

Do **not** use:

```go
due.Before(time.Now().Truncate(24 * time.Hour))
```

to decide civil business overdue status. Duration-based midnight truncation disagrees with `DATE` / `CURRENT_DATE` near local midnight in positive UTC offsets.

Use `internal/core/businessdate` (or equivalent civil Y/M/D compare against business “today” in `MEDCORE_BUSINESS_TIMEZONE`).

## Package

`internal/core/businessdate` — civil `Date`, `FromTime`, `TodayIn` / `TodayAt`, `IsOverdue`. No global mutable timezone.
