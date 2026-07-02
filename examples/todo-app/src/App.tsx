import { useState, useEffect } from 'react'
import type { OrbitConfig } from './types'
import { loadConfig, saveConfig, clearConfig } from './orbit'
import { ConnectPage } from './components/ConnectPage'
import { LoginPage } from './components/LoginPage'
import { TodoPage } from './components/TodoPage'
import { FilesPage } from './components/FilesPage'
import './App.css'

type Page = 'connect' | 'login' | 'todos' | 'files'

function parseHash() {
  const hash = window.location.hash.replace(/^#/, '')
  if (!hash) return {}
  const params = new URLSearchParams(hash)
  return {
    token: params.get('token'),
    error: params.get('error'),
  }
}

export default function App() {
  const { token: hashToken, error: hashError } = parseHash()
  const initialConfig: OrbitConfig = (() => {
    const saved = loadConfig() ?? { baseURL: '', app: '' }
    if (hashToken && saved.baseURL && saved.app) {
      saved.jwt = hashToken
      saveConfig(saved)
    } else if (hashError && saved.baseURL && saved.app) {
      saved.pendingError = hashError
      saveConfig(saved)
    }
    return saved
  })()

  const [config, setConfig] = useState<OrbitConfig>(initialConfig)
  const [page, setPage] = useState<Page>(() => {
    if (!initialConfig.baseURL || !initialConfig.app) return 'connect'
    if (!initialConfig.jwt) return 'login'
    return 'todos'
  })

  useEffect(() => {
    if (hashToken || hashError) {
      window.location.hash = ''
    }
  }, [])

  const handleConnect = (baseURL: string, app: string) => {
    const c: OrbitConfig = { baseURL, app }
    saveConfig(c)
    setConfig(c)
    setPage('login')
  }

  const handleLogin = (jwt: string) => {
    const c: OrbitConfig = { ...config, jwt }
    saveConfig(c)
    setConfig(c)
    setPage('todos')
  }

  const handleLogout = () => {
    clearConfig()
    setConfig({ baseURL: '', app: '' })
    setPage('connect')
  }

  switch (page) {
    case 'connect':
      return <ConnectPage initialBaseURL={config.baseURL} initialApp={config.app} onConnect={handleConnect} />
    case 'login':
      return <LoginPage config={config} onLogin={handleLogin} />
    case 'todos':
      return <TodoPage config={config} onLogout={handleLogout} onNavigate={setPage} />
    case 'files':
      return <FilesPage config={config} onLogout={handleLogout} onNavigate={setPage} />
  }
}
