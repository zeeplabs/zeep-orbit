# State

## Decisions

### D-001: Go for control plane
Go chosen over Node.js for the core server.
**Reason:** Single binary, zero runtime deps, trivial Docker image, strong concurrency primitives, good fit for infrastructure tooling.

### D-002: Own REST layer instead of PostgREST
Build the CRUD REST layer in Go, not delegate to PostgREST.
**Reason:** PostgREST requires one process per schema (or complex config), adds operational overhead, limits auth middleware customization. Own layer = one binary, full control, simpler deploy.
**Trade-off:** More code to write in M1. Can revisit PostgREST as optional engine in M2.

### D-003: Schema isolation via PostgreSQL schemas (not databases)
Each app gets its own PostgreSQL schema (`app_{name}`), not a separate database.
**Reason:** One PostgreSQL connection pool, no N-database management, schema-level isolation is sufficient for MVP. Row-level security can be added per schema in M3.
**Trade-off:** Schema-level is not as isolated as database-level. Acceptable for MVP.

### D-004: YAML config-first, no UI in M1
Apps defined in `apps.yaml`. No web UI for M1.
**Reason:** Fastest path to validate core value prop. UI adds weeks. YAML is good enough for technical users (the early adopters).

### D-005: JWT auth bring-your-own-secret per app
No built-in user management in M1. Each app has a JWT secret. Callers generate/verify tokens themselves.
**Reason:** Keeps zeep-core stateless on auth. No user table to maintain. Apps choose their own auth strategy.
**Trade-off:** More work for app developers. Acceptable — they already have auth in their frontend apps.

### D-006: CLI-first workflow
Primary interface is `zeep` CLI, not HTTP admin API.
**Reason:** Simpler to implement, natural fit for DevOps workflows, no auth needed for admin operations.

---

## Blockers

None.

---

## Todos

- [ ] Decide module name: `github.com/zeeplabs/zeep-core`
- [ ] Choose PostgreSQL migration approach: pure SQL files vs. programmatic via `pgx`
- [ ] Define filtering DSL for query params (PostgREST-compatible vs. custom)

---

## Lessons Learned

None yet.

---

## Deferred Ideas

- PostgREST as optional engine mode (M2)
- Per-row ownership via JWT `sub` mapped to `owner_id` column (M3)
- Schema versioning + rollback (M3)
- Multi-PostgreSQL cluster support
