# Known gotchas (consumer side)

- **"My update silently didn't take effect"** — you probably omitted a field on a partial-update call that Orbit's merge-on-absent-key semantics interpreted as "leave it alone." To clear a field, send it explicitly with an empty value/array; don't rely on omission.
- **"I can't change this column's allowed values / type"** — `enum` columns only accept their `allowed_values` at creation. Narrowing/widening an existing enum's values, or changing a column to/from `enum`, requires the dedicated endpoint/tool for that — not a generic table/column update call.
- **"GET returns an empty list but I know rows exist"** — check RLS policies for that table before assuming a client-side bug. RLS silently filters, it doesn't error.
- **"Error message is in English but my app is in Portuguese"** — expected. Orbit's API errors are always English by design; translate/display on the client side.
- **"I sent `id`/`created_at`/etc. in a create/update body and got a weird result"** — those are server-managed fields; strip them from the payload before sending, especially if you built the payload by spreading a previously-fetched row object.
- **"Docker image tag with a `v` prefix 404s"** — if you're pulling Orbit's own server image (not relevant to app-level integration, but common confusion): GHCR tags never carry the `v` from the git tag (`ghcr.io/zeeplabs/zeep-orbit:1.7.0`, not `:v1.7.0`).
