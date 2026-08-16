// safeReturnTo extracts and validates a `return_to` query param, used to
// resume a flow (e.g. the OAuth consent screen) that redirected an
// unauthenticated admin to /login. Only a same-origin relative path is ever
// honored — rejecting an absolute URL or a protocol-relative "//host/path"
// or "/\host/path" (browsers normalize a leading "/\" to "//", so both read
// as a scheme-relative redirect to another origin) prevents `return_to`
// itself from becoming an open-redirect vector.
//
// The returned path is always **relative to the router's `/dashboard`
// basename** (main.tsx's `BrowserRouter basename="/dashboard"`), regardless
// of which of the two shapes produced it: the SPA's own guards build it
// from `useLocation().pathname` (already basename-stripped), but the
// backend (oauth_server.go's unauthenticated Authorize branch) builds it
// from `r.URL.RequestURI()`, which is basename-*included*
// ("/dashboard/oauth/authorize?..."). Stripping that prefix here means
// every caller can uniformly do `/dashboard${returnTo}` for a full-page
// navigation without re-deriving which shape it got.
export function safeReturnTo(search: string): string | null {
  // URLSearchParams already percent-decodes the value — do not decode again
  // (the raw path/query it carries can itself contain "%" from a nested
  // param such as an encoded redirect_uri).
  let value = new URLSearchParams(search).get('return_to')
  if (!value) return null
  if (!value.startsWith('/') || value.startsWith('//') || value.startsWith('/\\')) return null
  if (value === '/dashboard' || value.startsWith('/dashboard/') || value.startsWith('/dashboard?')) {
    value = value.slice('/dashboard'.length) || '/'
  }
  if (!value.startsWith('/') || value.startsWith('//') || value.startsWith('/\\')) return null
  return value
}
