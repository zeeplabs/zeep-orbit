import { useState } from 'react'
import type { OrbitConfig } from './types'
import { loadConfig, saveConfig, clearConfig } from './orbit'
import { ConnectPage } from './components/ConnectPage'
import { LoginPage } from './components/LoginPage'
import { TodoPage } from './components/TodoPage'
import './App.css'

type Page = 'connect' | 'login' | 'todos'

export default function App() {
  const [config, setConfig] = useState<OrbitConfig>(() => loadConfig() ?? { baseURL: '', app: '' })
  const [page, setPage] = useState<Page>(() => {
    const saved = loadConfig()
    if (!saved?.baseURL || !saved?.app) return 'connect'
    if (!saved.jwt) return 'login'
    return 'todos'
  })

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
      return <TodoPage config={config} onLogout={handleLogout} />
  }
}
