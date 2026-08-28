import { PlatformAction } from '@/lib/permissions'

export interface NavItemDef {
  /** Material Symbols glyph. */
  icon: string
  /** chave i18n do label. */
  labelKey: string
  path: string
  /**
   * Ação de plataforma que gateia o item. Se ausente, item é visível a todos os
   * autenticados. A visibilidade é resolvida por `hasPlatformPermission` (matriz
   * em `@/lib/permissions`, que espelha `HasPlatformPermission` no Go) — fonte
   * única de verdade, com omit (não disable) por construção.
   *
   * Substitui o antigo `allow: Role[]`: ao invés de duplicar a matriz de papel
   * por item, declaramos qual ação de plataforma o item representa e a matriz
   * central decide.
   */
  platformAction?: PlatformAction
}

export interface NavSectionDef {
  titleKey: string
  items: NavItemDef[]
}

/**
 * Modelo de navegação gateado pela matriz de plataforma
 * (`.specs/features/dashboard-global-roles`, T-07). Backend é a fonte de verdade
 * do enforcement (retorna 403); aqui só derivamos visibilidade. Páginas com
 * rota direta continuam guardadas por `RequireRole` em `App.tsx`.
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
      { icon: 'key', labelKey: 'nav.mcp', path: '/mcp-settings' },
    ],
  },
  {
    titleKey: 'nav.sectionSuperadmin',
    items: [
      { icon: 'group', labelKey: 'nav.users', path: '/usuarios', platformAction: 'users' },
      { icon: 'shield', labelKey: 'nav.audit', path: '/auditoria', platformAction: 'audit' },
      { icon: 'extension', labelKey: 'nav.integrations', path: '/integracoes/github', platformAction: 'integrations' },
      { icon: 'settings', labelKey: 'nav.settings', path: '/configuracoes', platformAction: 'branding' },
    ],
  },
]

/** Itens fixos da tab bar mobile (Apps / Data Browser / Logs / SDKs). 5º slot é o botão "Mais". */
export const MOBILE_TABS: NavItemDef[] = [
  { icon: 'grid_view', labelKey: 'nav.apps', path: '/apps' },
  { icon: 'table', labelKey: 'nav.dataBrowser', path: '/data-browser' },
  { icon: 'analytics', labelKey: 'nav.logs', path: '/logs' },
  { icon: 'code', labelKey: 'SDKs', path: '/sdks' },
]
