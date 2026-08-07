import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useQuery } from '@tanstack/react-query'
import { PageHeader, EmptyState, LoadingState } from '@/components/patterns'
import { Button } from '@/components/ui/button'

const PAGE_SIZE = 10

type SectionType = 'features' | 'improvements' | 'fixes' | 'security' | 'breaking'

/** Campo bilíngue vindo direto do changelog.json — sem chaves i18n, texto fica no próprio dado. */
interface LocalizedText {
  en: string
  'pt-BR': string
}

interface SectionItem {
  description: LocalizedText
}

interface Section {
  type: SectionType
  items: SectionItem[]
}

interface ChangelogEntry {
  id?: string
  version: string
  release_date: string
  title: LocalizedText
  summary: LocalizedText
  sections: Section[]
}

function localize(text: LocalizedText, lang: string): string {
  return text[lang as keyof LocalizedText] || text.en
}

function parseSections(sections: unknown): Section[] {
  if (Array.isArray(sections)) return sections
  if (typeof sections === 'string') {
    try {
      return JSON.parse(sections)
    } catch {
      return []
    }
  }
  return []
}

/** Mapeia o tipo da seção (do backend) pro `typeColor` do handoff (linha 2831). */
const sectionTone: Record<SectionType, string> = {
  features: 'var(--primary)',
  improvements: 'var(--success)',
  fixes: 'var(--warning)',
  security: 'var(--danger)',
  breaking: 'var(--accent)',
}

function formatReleaseDate(dateStr: string, lang: string): string {
  const d = new Date(dateStr + 'T00:00:00Z')
  try {
    return new Intl.DateTimeFormat(lang, {
      day: '2-digit',
      month: 'short',
      year: 'numeric',
    }).format(d)
  } catch {
    return d.toLocaleDateString('en-US', { day: '2-digit', month: 'short', year: 'numeric' })
  }
}

function ChangelogEntryView({ entry }: { entry: ChangelogEntry }) {
  const { t, i18n } = useTranslation()
  const sections = parseSections(entry.sections)
  const dateLabel = formatReleaseDate(entry.release_date, i18n.language)

  return (
    <div className="flex gap-5">
      {/* Coluna esquerda: versão + data (90px fixo) */}
      <div className="w-[90px] shrink-0 pt-0.5">
        <div
          className="font-mono text-[13px] font-bold"
          style={{ color: 'var(--primary)' }}
        >
          {entry.version}
        </div>
        <div
          className="mt-0.5 text-[11.5px]"
          style={{ color: 'var(--text-tertiary)' }}
        >
          {dateLabel}
        </div>
      </div>
      {/* Coluna direita: conteúdo com border-left */}
      <div
        className="flex-1 border-l pl-5 pb-1"
        style={{ borderColor: 'var(--border)' }}
      >
        {entry.title && (
          <h3
            className="mb-1 text-base font-bold"
            style={{ color: 'var(--text-primary)', fontFamily: 'var(--font-display)' }}
          >
            {localize(entry.title, i18n.language)}
          </h3>
        )}
        {entry.summary && (
          <p
            className="mb-3.5 text-[13px]"
            style={{ color: 'var(--text-secondary)' }}
          >
            {localize(entry.summary, i18n.language)}
          </p>
        )}
        <div className="flex flex-col gap-2">
          {sections.map((section, si) => {
            const color = sectionTone[section.type] || sectionTone.fixes
            return (
              <div key={si} className="flex flex-col gap-2">
                {section.items.map((item, ii) => (
                  <div
                    key={ii}
                    className="flex items-start gap-2.5 text-[12.5px]"
                  >
                    <span
                      className="mt-0.5 shrink-0 rounded-full px-2 py-0.5 text-[10px] font-bold leading-none text-white opacity-90"
                      style={{ background: color }}
                    >
                      {t(`changelog.sectionType.${section.type}`)}
                    </span>
                    <span style={{ color: 'var(--text-secondary)' }}>
                      {localize(item.description, i18n.language)}
                    </span>
                  </div>
                ))}
              </div>
            )
          })}
        </div>
      </div>
    </div>
  )
}

export default function ChangelogPage() {
  const { t } = useTranslation()
  const [offset, setOffset] = useState(0)

  const { data, isLoading } = useQuery({
    queryKey: ['changelog'],
    queryFn: async () => {
      const res = await fetch('/dashboard/api/changelog', { credentials: 'include' })
      if (!res.ok) throw new Error('Failed to load')
      return res.json() as Promise<{ entries: ChangelogEntry[] }>
    },
    staleTime: 300000,
  })

  const allEntries = useMemo(() => data?.entries || [], [data?.entries])
  const entries = allEntries.slice(0, offset + PAGE_SIZE)
  const hasMore = offset + PAGE_SIZE < allEntries.length

  return (
    <>
      <PageHeader title={t('changelog.title')} subtitle={t('changelog.subtitle')} />

      <div className="mx-auto flex w-full max-w-[780px] flex-col gap-7">
        {isLoading ? (
          <LoadingState rows={4} />
        ) : entries.length === 0 ? (
          <EmptyState
            icon="campaign"
            title={t('changelog.empty')}
          />
        ) : (
          <>
            {entries.map((entry, i) => (
              <ChangelogEntryView key={entry.id || i} entry={entry} />
            ))}

            {hasMore && (
              <div className="mt-2">
                <Button
                  variant="outline"
                  onClick={() => setOffset((p) => p + PAGE_SIZE)}
                >
                  {t('changelog.loadMore')}
                </Button>
              </div>
            )}
          </>
        )}
      </div>
    </>
  )
}
