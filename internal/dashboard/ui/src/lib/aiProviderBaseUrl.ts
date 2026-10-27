// Client-side mirror of normalizeAIProviderBaseURL (internal/dashboard/ai_provider_baseurl.go),
// used only to give instant feedback. The server stays authoritative: it also rejects
// link-local / cloud-metadata hosts, which this check deliberately does not try to replicate,
// and its 400 message is surfaced to the user as-is.

const MAX_BASE_URL_LENGTH = 2048

export type AIProviderBaseUrlError = 'required' | 'tooLong' | 'invalid' | 'credentials' | 'query'

/** Returns null when `raw` looks acceptable, otherwise the reason (map it to an i18n key). */
export function validateAIProviderBaseUrl(raw: string): AIProviderBaseUrlError | null {
  const s = raw.trim()
  if (s === '') return 'required'
  if (s.length > MAX_BASE_URL_LENGTH) return 'tooLong'
  // Spaces and control characters have no place in a base URL.
  // eslint-disable-next-line no-control-regex
  if (/[\u0000- \u007f]/.test(s)) return 'invalid'

  let u: URL
  try {
    u = new URL(s)
  } catch {
    return 'invalid'
  }
  if (u.protocol !== 'http:' && u.protocol !== 'https:') return 'invalid'
  if (u.hostname === '') return 'invalid'
  if (u.username !== '' || u.password !== '') return 'credentials'
  if (s.includes('?') || s.includes('#')) return 'query'
  return null
}
