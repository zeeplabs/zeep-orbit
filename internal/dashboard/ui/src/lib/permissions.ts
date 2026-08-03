/**
 * Matriz de permissões de plataforma (dashboard-global-roles T-07).
 * Mirror EXATO de `HasPlatformPermission` em `internal/dashboard/platform_roles.go`
 * (T-02). Manter os dois em sincronia: se a matriz no backend mudar, atualizar
 * aqui também.
 *
 * O backend é a fonte de verdade de ENFORCEMENT (retorna 403). Aqui só
 * derivamos visibilidade de UI — omitir, não desabilitar. Nunca substitui o
 * check server-side; é só UX.
 */
import { Role } from './roles'

export type PlatformAction =
  | 'templates'
  | 'branding'
  | 'users'
  | 'integrations'
  | 'infra'
  | 'audit'
  | 'own_apps'

/**
 * Matriz: para cada PlatformAction, a lista de papéis que podem executá-la.
 * Qualquer papel não listado é negado (defensive default).
 *
 * Formato com `readonly Role[]` (em vez de `Set`) para que o JSON snapshot
 * de roles seja serializável e o array seja literal no bundle — não há
 * alocação de Set em runtime.
 */
export const PLATFORM_PERMISSIONS: Record<PlatformAction, readonly Role[]> = {
  templates: ['superadmin', 'admin'],
  branding: ['superadmin', 'admin'],
  users: ['superadmin', 'admin'],
  integrations: ['superadmin'],
  infra: ['superadmin'],
  audit: ['superadmin', 'auditor'],
  own_apps: ['superadmin', 'admin', 'member'],
}

/**
 * Versão pura (não-hook) de `hasPlatformPermission` — espelha a função Go.
 * Use diretamente quando já tiver o `role` em mãos (ex: dentro de um
 * componente que já chama `useCurrentRole`); prefira o hook
 * `useHasPlatformPermission` em componentes que não têm o role.
 */
export function hasPlatformPermission(role: Role, action: PlatformAction): boolean {
  return PLATFORM_PERMISSIONS[action].includes(role)
}
