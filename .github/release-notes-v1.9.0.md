## v1.9.0

Minor release adding native upsert support to the generic insert endpoint and classifying insert failures into proper HTTP status codes instead of a generic 500.

### Added

- **`POST /{app}/{table}` now accepts `on_conflict: "ignore" | "update"` with `conflict_columns: [...]`** for idempotent inserts against a unique constraint (e.g. retried webhook deliveries). `"ignore"` no-ops on a conflict (`200` with the existing row when `conflict_columns` was given, `204` otherwise); `"update"` upserts the conflicting row and requires `conflict_columns` explicitly, distinguishing a genuine insert (`201`) from a real upsert (`200`) via Postgres's own `xmax = 0` signal. Both modes run in a single transaction with the insert and apply the same owner-scoping, soft-delete exclusion, and native row-level-security policies every other read/write path on the table applies — a conflict belonging to another tenant, denied by a row policy, or colliding with the caller's own soft-deleted row is now consistently `409` across both modes instead of being disclosed, overwritten, silently resurrected, or falling through as an unclassified `500`. Invalid `conflict_columns` (unknown, duplicate, missing from the body, or not backed by a unique/exclusion constraint) is rejected with `400`. Documented in the generated OpenAPI spec and all 4 READMEs.

### Fixed

- **`POST /{app}/{table}` collapsed every insert failure Postgres didn't reject on `23514` (check_violation) into a generic `{"error":"failed to insert row"}` HTTP 500**, forcing integrators to read Orbit server logs to tell a duplicate row apart from an internal failure. A unique-constraint violation (`23505`) now responds `409` naming the constraint; an `on_conflict:"update"` collision a native Postgres row policy denies (`42501`) also responds `409`, while a plain insert rejected outright by a table's own `WITH CHECK` policy stays a generic `500` (it isn't a conflict). Any genuinely unclassified `500` is now also logged server-side with the app/table context.
- **An empty string `""` sent for a `timestamptz` column reached Postgres as-is and raised a raw, unclassified error.** It now normalizes to `NULL` for nullable `timestamptz` columns on both create and update; any other non-empty value is validated before the query runs — RFC3339, Postgres's own text format, a bare date, an offset-less or colon-less/short-UTC-offset timestamp, and `infinity`/`-infinity` are all accepted. Named time zone abbreviations (`UTC`, `America/Sao_Paulo`) and relative values (`now`, `epoch`) are now rejected with `400` — a narrow, intentional behavior change from Postgres's previous raw acceptance. A malformed value responds `400` naming the column, never leaking the raw value or a raw driver error.

### Upgrade notes

No migration needed. The only breaking behavior change: `timestamptz` values using a named time zone abbreviation or a relative keyword (`now`, `epoch`, `today`) are now rejected with `400` instead of being accepted raw by Postgres — most relevant to third-party webhook payloads sent directly to a table's insert endpoint. Use an explicit UTC offset instead.
