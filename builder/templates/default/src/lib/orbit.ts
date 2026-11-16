// The ONLY place that talks to the Orbit backend. Pages and components import
// from here; never call fetch() against the Orbit API directly and never
// hardcode the API URL or app name (they come from ./orbit.config.ts).
//
// The SDK (@zeeptech/orbit-client) takes a fixed `jwt` in its constructor and
// has no token storage or storage-injection option, so session handling lives
// here: the JWT is kept in a SafeStorage and the client is rebuilt whenever it
// changes.
import { OrbitClient } from '@zeeptech/orbit-client'
import { ORBIT_API_URL, ORBIT_APP_NAME } from '../orbit.config'
import { createSafeStorage } from './storage'

const TOKEN_KEY = 'orbit.token'
const storage = createSafeStorage()

/** False while orbit.config.ts still holds the placeholder values. */
export const isOrbitConfigured =
  !ORBIT_API_URL.includes('example.invalid') && ORBIT_APP_NAME !== 'your_app_name'

function buildClient(jwt: string): OrbitClient {
  return new OrbitClient({ baseURL: ORBIT_API_URL, app: ORBIT_APP_NAME, jwt })
}

let client = buildClient(storage.getItem(TOKEN_KEY) ?? '')

/** Current SDK client. Re-read it after login/logout; do not cache it. */
export function getOrbit(): OrbitClient {
  return client
}

export function hasSession(): boolean {
  return Boolean(storage.getItem(TOKEN_KEY))
}

export function setSessionToken(token: string | null): void {
  if (token) {
    storage.setItem(TOKEN_KEY, token)
  } else {
    storage.removeItem(TOKEN_KEY)
  }
  client = buildClient(token ?? '')
}

export async function signIn(email: string, password: string): Promise<void> {
  const { token } = await client.auth.login({ email, password })
  setSessionToken(token)
}

export async function signOut(): Promise<void> {
  try {
    await client.auth.logout()
  } finally {
    setSessionToken(null)
  }
}
