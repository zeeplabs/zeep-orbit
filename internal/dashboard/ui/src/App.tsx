import { useQuery, useQueryClient } from '@tanstack/react-query'
import LoginPage from './pages/LoginPage'
import DashboardShell from './pages/DashboardShell'
import OnboardingPage from './pages/OnboardingPage'
import { useBootstrapStatus } from './lib/api'

function App() {
  const qc = useQueryClient()

  const { data: status, isLoading: statusLoading } = useBootstrapStatus()

  const { data: user, isLoading: userLoading } = useQuery({
    queryKey: ['me'],
    queryFn: async () => {
      const res = await fetch('/dashboard/api/me', { credentials: 'include' })
      if (!res.ok) return null
      return res.json()
    },
    retry: false,
    enabled: status?.bootstrapped === true,
  })

  if (statusLoading || (status?.bootstrapped && userLoading)) {
    return (
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          height: '100vh',
          background: '#0A0A0F',
        }}
      >
        <div
          style={{
            color: 'rgba(255,255,255,0.4)',
            fontFamily: 'Plus Jakarta Sans, sans-serif',
          }}
        >
          Carregando...
        </div>
      </div>
    )
  }

  if (!status?.bootstrapped) {
    return (
      <OnboardingPage
        onComplete={() => qc.invalidateQueries({ queryKey: ['bootstrap-status'] })}
      />
    )
  }

  return user ? <DashboardShell user={user} /> : <LoginPage />
}

export default App
