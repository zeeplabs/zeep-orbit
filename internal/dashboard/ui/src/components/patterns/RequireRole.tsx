import { ReactNode } from 'react'
import { Role, roleAllowed } from '@/lib/roles'
import { useCurrentRole } from '@/lib/useCurrentRole'
import AccessDenied from '@/pages/AccessDenied'

/**
 * Guarda de rota por papel. Navegação direta a uma tela bloqueada renderiza a
 * 403 genérica (mantém a URL), nunca crash/tela branca (DRD-22). Complementa o
 * enforcement de backend — não o substitui.
 */
export function RequireRole({ allow, children }: { allow: Role[]; children: ReactNode }) {
  const role = useCurrentRole()
  if (!roleAllowed(role, allow)) return <AccessDenied />
  return <>{children}</>
}
