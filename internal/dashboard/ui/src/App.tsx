import LoginPage from './pages/LoginPage'
import DashboardShell from './pages/DashboardShell'
import { useQuery } from '@tanstack/react-query'

function App() {
  const { data: user, isLoading } = useQuery({
    queryKey: ['me'],
    queryFn: async () => {
      const res = await fetch('/dashboard/api/me', { credentials: 'include' })
      if (!res.ok) return null
      return res.json()
    },
    retry: false,
  })

  if (isLoading) {
    return (
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100vh' }}>
        <div style={{ color: 'var(--text-muted)' }}>Carregando...</div>
      </div>
    )
  }

  return user ? <DashboardShell user={user} /> : <LoginPage />
}

export default App
