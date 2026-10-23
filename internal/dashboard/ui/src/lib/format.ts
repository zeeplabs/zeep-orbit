import i18n from './i18n'

/**
 * Formatadores de data/hora do dashboard. Usam o idioma ativo da UI
 * (`i18n.language`) lido na hora da chamada — telas já re-renderizam ao trocar de
 * idioma (via `useTranslation`), então não precisam repassar `lang`.
 * Valor inválido devolve o texto original em vez de lançar RangeError no render.
 */
function format(iso: string, opts: Intl.DateTimeFormatOptions): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  return new Intl.DateTimeFormat(i18n.language || 'en', opts).format(d)
}

/** "20 de out. de 2026" / "Oct 20, 2026" */
export function formatDate(iso: string): string {
  return format(iso, { day: '2-digit', month: 'short', year: 'numeric' })
}

/** Hora com segundos, ex.: "10:01:05". */
export function formatTime(iso: string): string {
  return format(iso, { hour: '2-digit', minute: '2-digit', second: '2-digit' })
}

/** Data + hora (24h) sem segundos. */
export function formatDateTime(iso: string): string {
  return format(iso, {
    day: '2-digit',
    month: 'short',
    year: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  })
}

/** Data curta + hora com segundos — para valores `timestamptz` de dados do usuário. */
export function formatTimestamp(iso: string): string {
  return format(iso, { dateStyle: 'short', timeStyle: 'medium' })
}
