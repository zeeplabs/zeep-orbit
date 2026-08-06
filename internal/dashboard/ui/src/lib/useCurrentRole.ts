import { useQuery } from '@tanstack/react-query'
import { Role, normalizeRole } from './roles'

interface Me {
  role?: string
  [k: string]: unknown
}

/**
 * Papel do usuário logado, derivado da sessão (`/me`), normalizado para o enum
 * de 4 níveis. Fonte única de verdade de role na UI — nunca de switcher
 * client-side (o "Viewing as" do protótipo é aid de review, não feature).
 */
export function useCurrentRole(): Role {
  const { data } = useQuery<Me | null>({
    queryKey: ['me'],
    // reaproveita o cache populado por App.tsx; não refaz fetch aqui
    enabled: false,
  })
  return normalizeRole(data?.role)
}
