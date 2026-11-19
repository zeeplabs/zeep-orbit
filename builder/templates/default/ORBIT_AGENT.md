# Orbit frontend template - guide for the AI agent

## Stack
Vite 7, React 19, TypeScript (strict), React Router 7, plain CSS (`src/index.css`),
`@zeeptech/orbit-client` for the backend. No Tailwind or UI kit: write small
components and use the CSS variables in `src/index.css`.

## Layout
- `src/main.tsx` - entry; `HashRouter` (the preview is served under a changing URL path). Do not replace it with `BrowserRouter`.
- `src/App.tsx` - layout and `<Routes>`. Register every new page here.
- `src/pages/` - one file per route (`FooPage.tsx`).
- `src/components/` - reusable UI.
- `src/lib/orbit.ts` - the only module that talks to the backend.
- `src/lib/storage.ts` - storage that falls back to memory when browser storage is blocked.
- `src/orbit.config.ts` - generated (API URL, app name). Never edit.
- `index.html`, `vite.config.ts`, `tsconfig*.json`, `eslint.config.js`, `package*.json`,
  `orbit-template.json`, `TEMPLATE_VERSION` - build plumbing; leave them alone unless the task requires it.

## Calling the backend
```ts
import { getOrbit, signIn, signOut, hasSession } from '../lib/orbit'

const page = await getOrbit().table('tasks').findMany({ filters: { status: 'eq.open' }, limit: 20 })
await getOrbit().table('tasks').create({ title: 'Buy milk' })
await signIn(email, password) // stores the JWT; call getOrbit() again afterwards
```
- Always call `getOrbit()` at use time; the client is rebuilt on login/logout.
- Table methods: `findMany`, `findById`, `create`, `update`, `replace`, `delete`. Files: `getOrbit().files`.
- `findMany` returns `{ data, count, limit, offset }`. An empty list can mean the user's
  access policy hides rows, not that the table is empty.
- Do not send `id`, `created_at`, `updated_at`, `owner_id` in create/update bodies.
- Handle errors with try/catch and show a message; never swallow them.

## Rules
- Never hardcode secrets, tokens, API URLs or the app name. Use `src/lib/orbit.ts`.
- Never call `localStorage`/`sessionStorage` directly; use `createSafeStorage()`.
- The app runs under a changing sub-path in preview (the build base is relative):
  import assets from `src/` or use relative paths, never absolute `/something` URLs
  for assets (use `<Link to="/x">` for navigation).
- No new dependencies unless the task cannot be done without them.
- Text files only. Do not touch `node_modules`, `dist`, `.git`.
- Keep `npm run lint` and `npx tsc -b` green before finishing a turn.
- UI copy follows the user's language; code, identifiers and comments in English.
