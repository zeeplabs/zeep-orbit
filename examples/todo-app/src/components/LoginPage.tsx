import { useState } from 'react'
import type { OrbitConfig } from '../types'
import { createClient } from '../orbit'

interface LoginPageProps {
  config: OrbitConfig
  onLogin: (jwt: string) => void
}

export function LoginPage({ config, onLogin }: LoginPageProps) {
  const [mode, setMode] = useState<'login' | 'register'>('login')
  const [name, setName] = useState('')
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError('')
    setLoading(true)

    try {
      const client = createClient(config)

      if (mode === 'register') {
        await client.auth.register({ email, password, name })
        const { token } = await client.auth.login({ email, password })
        onLogin(token)
      } else {
        const { token } = await client.auth.login({ email, password })
        onLogin(token)
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Authentication failed')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="page">
      <div className="card">
        <h1>{mode === 'login' ? 'Sign in' : 'Create account'}</h1>
        <p className="subtitle">
          {config.baseURL}/{config.app}
        </p>

        <form onSubmit={handleSubmit}>
          {mode === 'register' && (
            <label>
              Name
              <input
                type="text"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="Your name"
              />
            </label>
          )}

          <label>
            Email
            <input
              type="email"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              placeholder="you@example.com"
              required
            />
          </label>

          <label>
            Password
            <input
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              placeholder="••••••••"
              required
              minLength={6}
            />
          </label>

          {error && <p className="error">{error}</p>}

          <button type="submit" className="btn-primary" disabled={loading}>
            {loading ? 'Please wait…' : mode === 'login' ? 'Sign in' : 'Create account'}
          </button>
        </form>

        <p className="switch-mode">
          {mode === 'login' ? (
            <>
              Don't have an account?{' '}
              <button className="link" onClick={() => setMode('register')}>
                Create one
              </button>
            </>
          ) : (
            <>
              Already have an account?{' '}
              <button className="link" onClick={() => setMode('login')}>
                Sign in
              </button>
            </>
          )}
        </p>
      </div>
    </div>
  )
}
