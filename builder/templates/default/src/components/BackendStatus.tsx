import { useState } from 'react'
import { getOrbit, isOrbitConfigured } from '../lib/orbit'
import { ORBIT_APP_NAME } from '../orbit.config'

// Example of reading from the Orbit backend through the SDK. Replace
// "your_table" with a real table of the backend app when you build a page.
export default function BackendStatus() {
  const [state, setState] = useState<'idle' | 'loading' | 'ok' | 'error'>('idle')
  const [message, setMessage] = useState('')

  if (!isOrbitConfigured) {
    return <p className="muted">Backend not configured yet (orbit.config.ts holds placeholders).</p>
  }

  async function check() {
    setState('loading')
    try {
      const page = await getOrbit().table('your_table').findMany({ limit: 5 })
      setMessage(`${page.count} row(s) in total`)
      setState('ok')
    } catch (err) {
      setMessage(err instanceof Error ? err.message : 'Unknown error')
      setState('error')
    }
  }

  return (
    <div>
      <p className="muted">Connected app: {ORBIT_APP_NAME}</p>
      <button type="button" onClick={() => void check()} disabled={state === 'loading'}>
        {state === 'loading' ? 'Checking...' : 'Test backend call'}
      </button>
      {state !== 'idle' && state !== 'loading' && <p className={state}>{message}</p>}
    </div>
  )
}
