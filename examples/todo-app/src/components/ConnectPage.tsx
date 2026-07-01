import { useState } from 'react'

interface ConnectPageProps {
  initialBaseURL: string
  initialApp: string
  onConnect: (baseURL: string, app: string) => void
}

export function ConnectPage({ initialBaseURL, initialApp, onConnect }: ConnectPageProps) {
  const [baseURL, setBaseURL] = useState(initialBaseURL)
  const [app, setApp] = useState(initialApp)

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    if (baseURL && app) onConnect(baseURL, app)
  }

  return (
    <div className="page">
      <div className="card">
        <h1>Zeep Orbit</h1>
        <p className="subtitle">Connect to your Orbit instance</p>
        <form onSubmit={handleSubmit}>
          <label>
            Base URL
            <input
              type="url"
              value={baseURL}
              onChange={(e) => setBaseURL(e.target.value)}
              placeholder="http://localhost:8080"
              required
            />
          </label>
          <label>
            App Name
            <input
              type="text"
              value={app}
              onChange={(e) => setApp(e.target.value)}
              placeholder="my-app"
              required
            />
          </label>
          <button type="submit" className="btn-primary">
            Connect
          </button>
        </form>
      </div>
    </div>
  )
}
