import { useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'

// OAuthConsent renders after Authorize (backend) has already validated
// client_id/redirect_uri and confirmed an active session — it names the
// requesting client and shows the redirect URI's *origin* (not just the
// client's self-declared name), mitigating the phishing-adjacent risk
// design.md's Risks & Concerns table flags for this screen: an admin sees
// where they're actually being sent, not just a string the client chose.
export default function OAuthConsent() {
  const { t } = useTranslation()
  const [params] = useSearchParams()
  const [loading, setLoading] = useState<'grant' | 'deny' | null>(null)

  const clientId = params.get('client_id') ?? ''
  const clientName = params.get('client_name') ?? clientId
  const redirectUri = params.get('redirect_uri') ?? ''
  const codeChallenge = params.get('code_challenge') ?? ''
  const codeChallengeMethod = params.get('code_challenge_method') ?? ''
  const state = params.get('state') ?? ''

  let redirectOrigin = redirectUri
  try {
    redirectOrigin = new URL(redirectUri).origin
  } catch {
    // Malformed redirect_uri would already have been rejected by Authorize
    // before this page ever rendered — fall back to showing it verbatim.
  }

  const decide = async (decision: 'grant' | 'deny') => {
    setLoading(decision)
    try {
      const res = await fetch('/dashboard/oauth/authorize', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          client_id: clientId,
          redirect_uri: redirectUri,
          code_challenge: codeChallenge,
          code_challenge_method: codeChallengeMethod,
          state,
          decision,
        }),
      })
      const data = await res.json()
      if (!res.ok) {
        toast.error(data.error || t('oauthConsent.error'))
        return
      }
      // Defense in depth: the backend now rejects non-https/non-loopback
      // redirect_uris at registration, but this screen still guards the
      // navigation itself before handing an untrusted URL to the browser.
      let scheme = ''
      try {
        scheme = new URL(data.redirect_url).protocol
      } catch {
        toast.error(t('oauthConsent.error'))
        return
      }
      if (scheme !== 'https:' && scheme !== 'http:') {
        toast.error(t('oauthConsent.error'))
        return
      }
      window.location.href = data.redirect_url
    } catch {
      toast.error(t('common.connectionError'))
    } finally {
      setLoading(null)
    }
  }

  return (
    <div
      className="flex min-h-screen items-center justify-center"
      style={{ background: 'var(--bg-page)' }}
    >
      <div
        className="w-full max-w-md rounded-[12px] p-8"
        style={{ background: 'var(--surface-raised)', border: '1px solid var(--border)' }}
      >
        <h1 className="mb-2 text-lg font-semibold" style={{ color: 'var(--text-primary)' }}>
          {t('oauthConsent.title')}
        </h1>
        <p className="mb-1" style={{ color: 'var(--text-secondary)' }}>
          {t('oauthConsent.clientName', { name: clientName })}
        </p>
        <p className="mb-6 text-sm" style={{ color: 'var(--text-tertiary)' }}>
          {t('oauthConsent.redirectOrigin', { origin: redirectOrigin })}
        </p>
        <div className="flex gap-3">
          <Button
            variant="outline"
            className="flex-1"
            disabled={loading !== null}
            onClick={() => decide('deny')}
          >
            {loading === 'deny' ? t('oauthConsent.denying') : t('oauthConsent.deny')}
          </Button>
          <Button
            className="flex-1"
            disabled={loading !== null}
            onClick={() => decide('grant')}
          >
            {loading === 'grant' ? t('oauthConsent.granting') : t('oauthConsent.grant')}
          </Button>
        </div>
      </div>
    </div>
  )
}
