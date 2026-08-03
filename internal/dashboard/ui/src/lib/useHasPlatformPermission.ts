import { useCurrentRole } from './useCurrentRole'
import { PlatformAction, hasPlatformPermission } from './permissions'

/**
 * Hook que retorna se o papel logado pode executar a `action` de plataforma.
 * Equivalente client-side de `HasPlatformPermission` (Go, T-02) — usado para
 * omitir (não desabilitar) UI condicional dentro de uma página.
 *
 * Princípio da spec: **omitir, não desabilitar** — item some da renderização,
 * não fica cinza/desabilitado (vazamento de existência de feature que o
 * usuário não pode usar).
 *
 * Nunca substitui o enforcement de backend; é só visibilidade de UI. O 403
 * no servidor é a fonte de verdade; este hook é para que o usuário nem
 * veja o botão/link que o servidor rejeitaria.
 */
export function useHasPlatformPermission(action: PlatformAction): boolean {
  const role = useCurrentRole()
  return hasPlatformPermission(role, action)
}
