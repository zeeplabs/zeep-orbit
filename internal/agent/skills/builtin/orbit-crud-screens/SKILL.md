---
name: orbit-crud-screens
description: Use when building list, detail and edit screens over Orbit tables: schema first, pagination, filters and search, safe payloads, and empty lists that may be hidden by access policies.
version: 1
source: Written for the Orbit builder (Zeep Orbit); the behavior described is the Orbit table REST API and its TypeScript client
---

# CRUD screens over Orbit tables

A list, a detail view and a form for one table. Build them against the real schema, with the Orbit client, and register the routes.

## 1. Start from the schema

- Call `orbit_get_app_schema` before writing code. Use the exact table and column names, column types, nullability, enum values and relations it returns. Do not invent columns, and do not fake rows.
- If the table or column you need does not exist, call `propose_backend_change` with the smallest change and tell the user that the screen waits for their confirmation. Never hardcode sample data as if it were real.
- Plan the files: a list page, a form component used by "new" and "edit", and routes in `src/App.tsx` (`/customers`, `/customers/new`, `/customers/:id`). Link between them with `<Link>`.

## 2. List screen

```tsx
const PAGE_SIZE = 20

const page = await getOrbit().table('customers').findMany({
  filters: { status: 'eq.active', name: 'ilike.%ana%' },
  order: 'created_at.desc',
  limit: PAGE_SIZE,
  offset: pageIndex * PAGE_SIZE,
})
// page = { data, count, limit, offset }
```

- Page the data. Default to 20 rows. The server maximum is 1000 and the default is 50, so never ask for "everything". Show "Page X of Y" or Previous and Next, using `count`, and disable the buttons at the ends. Go back to the first page when a filter changes.
- Filters are strings in the form `operator.value`. The operators are `eq.`, `ne.`, `gt.`, `gte.`, `lt.`, `lte.`, `like.` and `ilike.`. Search is `ilike.%term%`. Debounce a search box by about 300 ms, and ignore responses that arrive late.
- Order with `order: 'column.asc'` or `'column.desc'`, on a column that exists.
- Show the columns that identify a row, not every column. Format dates, money and booleans for the user's locale.
- Handle the four states: loading, error with retry, empty, ready.
- **An empty list may not mean the table is empty.** Row-level security filters rows silently, so a user can see zero rows that exist. Word the empty state neutrally ("No customers to show") and do not conclude that the table has no data or that the screen is broken.

## 3. Detail and edit

- Load one row with `findById(id)`. Handle "not found" and "no access" with a clear message and a link back to the list.
- The form fields come from the schema: text for text columns, number inputs for numeric ones, a checkbox for booleans, date inputs for dates, a select for enum columns using the allowed values from the schema.
- For a column that references another table, offer a select. Load the options from the referenced table, show a readable column (a name or title), and send the id.
- Respect nullability: required columns are required in the form; optional ones may be empty.

## 4. Writing data

- Build the body from the form values only. Never spread a fetched row into the body, and never send `id`, `created_at`, `updated_at`, `owner_id` or `deleted_at`: they are managed by the server.
- Create with `create(body)`. Update with `update(id, body)`: it is a partial update, so only send the fields that changed. Fields you omit are left as they are, so to clear a value send it explicitly (an empty optional value as `null`).
- Convert strings to the column type before sending (numbers, booleans, `null` for an empty optional field).
- After a successful write, refresh the list or navigate to it, and confirm the result in words.
- Delete with `delete(id)` after a confirmation that names the item. The server may soft-delete or hard-delete, so remove the row from the view and refetch.

## 5. Errors to expect

The client throws an `Error` with the server's English message. Recognize these and show your own text in the user's language:

- A required column is missing, a value is not allowed for a column, or a value is invalid for its type: highlight the field when the message names a column.
- The row already exists (unique constraint): tell the user the value is already used.
- A referenced row does not exist (insert or update), or the row is still referenced by another table (delete): explain it in plain words.
- A row-level security policy denied the write: "You do not have permission to do this."
- Anything else: a generic message with a retry. Do not expose internal details.

## 6. Files and ownership

- For file columns or uploads use `getOrbit().files`: upload, then show the file through a signed URL from the client rather than building a storage address yourself.
- The `owner_id` of a row is set by the server. Do not ask the user for it, and do not filter by it unless the schema and the task need that.

## 7. Finish

Run `run_build` for `typecheck`, `lint` and `build`, and fix each failure. Tell the user which screens exist and what they can do, in plain language.
