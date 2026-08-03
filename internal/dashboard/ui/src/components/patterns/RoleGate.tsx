import { ReactNode } from 'react'
import { Role, roleAllowed } from '@/lib/roles'
import { useCurrentRole } from '@/lib/useCurrentRole'

interface RoleGateProps {
  /** Papéis que podem ver o conteúdo. */
  allow: Role[]
  children: ReactNode
  /** Renderizado quando o papel não está em `allow` (default: nada — omitir). */
  fallback?: ReactNode
}

/**
 * Esconde conteúdo fora da permissão do papel logado. Princípio da spec:
 * **omitir, não desabilitar** — item some da renderização, não fica cinza.
 * Nunca é substituto do enforcement de backend; é só visibilidade de UI.
 */
export function RoleGate({ allow, children, fallback = null }: RoleGateProps) {
  const role = useCurrentRole()
  return <>{roleAllowed(role, allow) ? children : fallback}</>
}
