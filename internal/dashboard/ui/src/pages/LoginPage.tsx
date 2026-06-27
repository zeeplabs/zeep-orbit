import { useState } from 'react'
import { motion } from 'framer-motion'
import { useQueryClient } from '@tanstack/react-query'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'

export default function LoginPage() {
  const qc = useQueryClient()
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError('')
    setLoading(true)
    try {
      const res = await fetch('/dashboard/api/login', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ email, password }),
      })
      if (!res.ok) {
        const data = await res.json()
        setError(data.error || 'Credenciais inválidas')
        return
      }
      qc.invalidateQueries({ queryKey: ['me'] })
    } catch {
      setError('Erro de conexão')
    } finally {
      setLoading(false)
    }
  }

  const inputClass = 'h-10 rounded-lg bg-white/[0.05] border border-white/[0.10] px-4 py-2.5 text-sm text-[#F8FAFC] placeholder:text-white/30 outline-none focus-visible:ring-2 focus-visible:ring-[#0347A5]/40 focus-visible:border-[#0347A5]/60 transition-colors'

  return (
    <div className="flex min-h-screen items-center justify-center"
      style={{
        background: 'radial-gradient(ellipse at 20% 50%, rgba(3,71,165,0.15) 0%, transparent 50%), radial-gradient(ellipse at 80% 20%, rgba(124,58,237,0.15) 0%, transparent 50%), var(--bg)',
      }}
    >
      <motion.div
        initial={{ opacity: 0, y: 20 }}
        animate={{ opacity: 1, y: 0 }}
        transition={{ duration: 0.5, ease: [0.32, 0.72, 0, 1] }}
        className="w-[400px] border border-white/[0.10] bg-[#0D0D14]/60 backdrop-blur-xl rounded-2xl shadow-[0_0_40px_rgba(3,71,165,0.10)] p-8"
      >
        {/* Header */}
        <div className="flex items-center gap-3 mb-8">
          <div className="flex size-10 items-center justify-center rounded-xl border border-white/[0.08] bg-gradient-to-br from-[#0347A5]/15 to-[#7C3AED]/15 text-[16px] font-extrabold text-[#B3D1FF]">
            Z
          </div>
          <div>
            <h1 className="text-lg font-bold text-[#F8FAFC]">Zeep Dashboard</h1>
            <p className="text-[13px] text-[#94A3B8] mt-0.5">Entre com sua conta</p>
          </div>
        </div>

          <form onSubmit={handleSubmit} className="flex flex-col gap-4">
            <input
              type="email"
              placeholder="Email"
              value={email}
              onChange={e => setEmail(e.target.value)}
              required
              className={inputClass}
            />
            <input
              type="password"
              placeholder="Senha"
              value={password}
              onChange={e => setPassword(e.target.value)}
              required
              className={inputClass}
            />

            {error && (
              <p className="text-[13px] text-red-400 bg-red-500/[0.08] border border-red-500/[0.20] rounded-lg px-3 py-2">
                {error}
              </p>
            )}

            <Button
              type="submit"
              disabled={loading}
              className={cn(
                'h-10 rounded-lg text-sm font-bold text-white border-0 mt-1',
                loading
                  ? 'bg-gradient-to-br from-[#0347A5]/50 to-[#7C3AED]/50 cursor-not-allowed'
                  : 'bg-gradient-to-br from-[#0347A5] to-[#7C3AED] cursor-pointer hover:opacity-90',
              )}
            >
              {loading ? 'Entrando...' : 'Entrar'}
            </Button>
          </form>
      </motion.div>
    </div>
  )
}
