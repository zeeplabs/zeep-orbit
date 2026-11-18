import { Navigate, useLocation } from 'react-router-dom'

/**
 * `/login` with the current screen as `return_to`, so LoginPage (through `safeReturnTo`)
 * brings the user back to it after signing in. `pathname` is router-relative (no
 * `/dashboard` basename), the shape `safeReturnTo` expects. `/` needs no return_to, and a
 * path `safeReturnTo` would refuse as another origin is never put in the URL.
 */
export function loginPath(pathname: string, search: string): string {
  if (pathname === '/' && !search) return '/login'
  if (!pathname.startsWith('/') || pathname.startsWith('//') || pathname.startsWith('/\\')) return '/login'
  return `/login?return_to=${encodeURIComponent(pathname + search)}`
}

/** Replaces a bare `<Navigate to="/login" />` in a guard that found no session. */
export function RedirectToLogin() {
  const { pathname, search } = useLocation()
  return <Navigate to={loginPath(pathname, search)} replace />
}
