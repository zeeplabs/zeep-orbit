// safeReturnTo extracts and validates a `return_to` query param, used to
// resume a flow (e.g. the OAuth consent screen) that redirected an
// unauthenticated admin to /login. Only a same-origin relative path is ever
// honored — rejecting an absolute URL or a protocol-relative "//host/path"
// (which browsers treat as a scheme-relative redirect to another origin)
// prevents `return_to` itself from becoming an open-redirect vector.
export function safeReturnTo(search: string): string | null {
  // URLSearchParams already percent-decodes the value — do not decode again
  // (the raw path/query it carries can itself contain "%" from a nested
  // param such as an encoded redirect_uri).
  const value = new URLSearchParams(search).get('return_to')
  if (!value) return null
  if (!value.startsWith('/') || value.startsWith('//')) return null
  return value
}
