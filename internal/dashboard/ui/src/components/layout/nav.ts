import { Role } from '@/lib/roles'

export interface NavItemDef {
  /** Material Symbols glyph. */
  icon: string
  /** chave i18n do label. */
  labelKey: string
  path: string
  /** Papéis que veem o item. undefined = todos autenticados. */
  allow?: Role[]
}

export interface NavSectionDef {
  titleKey: string
  items: NavItemDef[]
}

/**
 * Modelo de navegação com gate por papel (final, 4 níveis). Ver
 * ui-design-brief de `dashboard-global-roles` para a matriz.
 *
 * Nota de interim: enquanto o backend não migra para 4 papéis, na prática só
 * existe `superadmin` (vê tudo). Um eventual `admin`/`auditor` legado pode ver
 * itens que o backend ainda 403a — resolve-se sozinho quando dashboard-global-roles
 * subir. `allow` já reflete a matriz final.
 */
export const NAV_SECTIONS: NavSectionDef[] = [
  {
    titleKey: 'nav.sectionGeneral',
    items: [
      { icon: 'grid_view', labelKey: 'nav.apps', path: '/apps' },
      { icon: 'table', labelKey: 'nav.dataBrowser', path: '/data-browser' },
    ],
  },
  {
    titleKey: 'nav.sectionDeployment',
    items: [
      { icon: 'analytics', labelKey: 'nav.logs', path: '/logs' },
      { icon: 'code', labelKey: 'SDKs', path: '/sdks' },
    ],
  },
  {
    titleKey: 'nav.sectionSuperadmin',
    items: [
      { icon: 'group', labelKey: 'nav.users', path: '/usuarios', allow: ['superadmin', 'admin'] },
      { icon: 'shield', labelKey: 'nav.audit', path: '/auditoria', allow: ['superadmin', 'auditor'] },
      { icon: 'extension', labelKey: 'nav.integrations', path: '/integracoes/github', allow: ['superadmin'] },
      { icon: 'settings', labelKey: 'nav.settings', path: '/configuracoes', allow: ['superadmin', 'admin'] },
    ],
  },
]

export const CHANGELOG_ITEM: NavItemDef = {
  icon: 'campaign',
  labelKey: 'nav.changelog',
  path: '/changelog',
}

/** Itens fixos da tab bar mobile (README: Apps / Data / Logs). */
export const MOBILE_TABS: NavItemDef[] = [
  { icon: 'grid_view', labelKey: 'nav.apps', path: '/apps' },
  { icon: 'table', labelKey: 'nav.dataBrowser', path: '/data-browser' },
  { icon: 'analytics', labelKey: 'nav.logs', path: '/logs' },
]
