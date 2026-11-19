---
name: orbit-react-patterns
description: Use when writing React components, state, effects, routes or data fetching through getOrbit(): hook hygiene, loading and error handling, typing rows, no secrets or direct storage in client code.
version: 1
source: Written for the Orbit builder (Zeep Orbit); conventions come from the default template and the Orbit client
---

# React patterns for Orbit frontends

The template is React, TypeScript in strict mode, React Router and plain CSS. These patterns keep generated code small, typed and safe to build. The template guide and the company standards win over anything here.

## Structure

- One component per file, named like the file. Pages live in `src/pages/` (`InvoicesPage.tsx`), reusable pieces in `src/components/`. Register every new page in `src/App.tsx`.
- Keep components small. When a component fetches, formats and renders, split the fetching into a custom hook (`useInvoices`) in the same feature folder.
- Type props with an `interface`. Do not use `any`, `@ts-ignore` or `eslint-disable` to silence errors: fix the cause.
- Lists need a stable `key` (the row `id`), never the array index when items can be reordered or removed.

## State

- Keep the smallest state that cannot be derived. Compute filtered lists, totals and labels during render instead of storing them in state and syncing with an effect.
- Use the functional update form when the next state depends on the previous one: `setCount((n) => n + 1)`.
- Controlled inputs: `value` plus `onChange`, with the value always a string in state. Convert to numbers only when you build the request.
- Reset a component's state by changing its `key`, not with an effect.

## Effects

- An effect synchronizes with something outside React (a request, a subscription, a timer). Do not use one for derived values or for responding to a click: put that logic in the event handler.
- Always declare the real dependencies. If a function is used in an effect, define it inside the effect or make it stable.
- Clean up: ignore a late response after unmount or after the inputs changed.

```tsx
function useRows(table: string, status: string) {
  const [state, setState] = useState<{ rows: Row[]; loading: boolean; error: string | null }>({
    rows: [],
    loading: true,
    error: null,
  })

  useEffect(() => {
    let cancelled = false
    setState((s) => ({ ...s, loading: true, error: null }))
    getOrbit()
      .table(table)
      .findMany({ filters: status ? { status: `eq.${status}` } : undefined, limit: 50 })
      .then((page) => {
        if (!cancelled) setState({ rows: page.data as Row[], loading: false, error: null })
      })
      .catch((err: unknown) => {
        if (!cancelled) setState((s) => ({ ...s, loading: false, error: errorMessage(err) }))
      })
    return () => {
      cancelled = true
    }
  }, [table, status])

  return state
}
```

Pass primitive values (strings, numbers) as dependencies, so the dependency list is honest and no lint suppression is needed.

## Talking to the backend

- All backend access goes through `src/lib/orbit.ts`. Call `getOrbit()` where you use it, never at module level: the client is rebuilt on sign-in and sign-out.
- Use the table methods `findMany`, `findById`, `create`, `update`, `replace`, `delete` and `getOrbit().files`. Never use raw `fetch`, a hardcoded API URL or the app name.
- Rows come back as records. Define an interface for the table from the real schema (call `orbit_get_app_schema` first) and cast once at the boundary, in the hook, not all over the UI.
- Never spread a fetched row into a create or update body: strip `id`, `created_at`, `updated_at`, `owner_id` and `deleted_at`. Build the body from the form fields.
- The client throws an `Error` whose message is the server's English text. Show your own message in the user's language for the cases you expect, and a generic one otherwise. Do not show raw server text for unexpected failures. Never swallow an error.

```tsx
export function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : 'Something went wrong'
}
```

## Routing and auth

- Navigation uses `<Link to="/path">` or `useNavigate()`. Read parameters with `useParams()`. The app uses `HashRouter`: do not replace it, and do not use absolute `/path` URLs for assets.
- Session helpers (`signIn`, `signOut`, `hasSession`) come from `src/lib/orbit.ts`. After signing in or out, call `getOrbit()` again. Guard a private route with a small wrapper component that redirects with `<Navigate to="/login" replace />` when `hasSession()` is false.
- Do not call `localStorage` or `sessionStorage` directly. Use the safe storage helper the template provides.

## Safety

- Nothing in the app is secret. Never put API keys, tokens, passwords or connection strings in code. The only credential is the signed-in user's session handled by `src/lib/orbit.ts`.
- Do not render HTML from data with `dangerouslySetInnerHTML`. Render text and let React escape it.
- Do not add third-party scripts, analytics or remote assets unless the user asks for a specific integration.

## Before you finish

Run `run_build` with `typecheck`, then `lint`, then `build`. Fix each failure at its cause and run it again.
