/**
 * Papéis globais de plataforma (redesign). Eixo definido em
 * `.specs/features/dashboard-global-roles/`. O enforcement é backend; aqui só
 * derivamos visibilidade de UI (omitir, não desabilitar).
 */
export type Role = 'superadmin' | 'admin' | 'auditor' | 'member'

export const ALL_ROLES: Role[] = ['superadmin', 'admin', 'auditor', 'member']

/**
 * Normaliza o valor de `role` vindo da sessão (`/me`) para o enum de 4 níveis.
 * Enquanto `dashboard-global-roles` não migrou o backend, `/me` ainda pode
 * retornar o modelo antigo (`admin`/`superadmin`). Nesse modelo, `admin` é o
 * papel mais permissivo não-super — mapeia direto para o novo `admin` (que tem
 * acesso de plataforma), preservando o comportamento atual sem crash.
 */
export function normalizeRole(raw: string | null | undefined): Role {
  switch (raw) {
    case 'superadmin':
      return 'superadmin'
    case 'admin':
      return 'admin'
    case 'auditor':
      return 'auditor'
    case 'member':
      return 'member'
    default:
      // valor desconhecido/legado: trata como o menos privilegiado
      return 'member'
  }
}

/** Papel logado tem acesso se estiver na lista permitida. */
export function roleAllowed(role: Role, allow: Role[]): boolean {
  return allow.includes(role)
}
