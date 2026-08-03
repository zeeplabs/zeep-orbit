import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { PageHeader } from '@/components/patterns'
import { Icon } from '@/components/ui/icon'
import { cn } from '@/lib/utils'

interface SdkEntry {
  name: string
  pkg: string
  install: string
  snippet: string
}

const sdks: SdkEntry[] = [
  {
    name: 'TypeScript',
    pkg: '@zeeptech/orbit-client',
    install: 'npm install @zeeptech/orbit-client',
    snippet: `import { OrbitClient } from '@zeeptech/orbit-client'

const orbit = new OrbitClient({
  baseURL: 'https://orbit.zeeplabs.com',
  app: 'my_app',
  jwt: 'token',
})

const rows = await orbit.table('invoices').findMany({ limit: 10 })`,
  },
  {
    name: 'Go',
    pkg: 'github.com/zeeplabs/orbit-go',
    install: 'go get github.com/zeeplabs/orbit-go',
    snippet: `import "github.com/zeeplabs/orbit-go"

client := orbit.New(orbit.ClientConfig{
    BaseURL: "https://orbit.zeeplabs.com",
    App:     "my_app",
    JWT:     "token",
})

rows, _ := client.Table("invoices").FindMany(ctx, &orbit.FindManyParams{Limit: 10})`,
  },
  {
    name: 'Python',
    pkg: 'zeeplabs-orbit-client',
    install: 'pip install zeeplabs-orbit-client',
    snippet: `from zeeplabs_orbit_client import OrbitClient, ClientConfig

orbit = OrbitClient(ClientConfig(
    base_url="https://orbit.zeeplabs.com",
    app="my_app",
    jwt="token",
))

rows = orbit.table("invoices").find_many(limit=10)`,
  },
  {
    name: 'Rust',
    pkg: 'zeep-orbit-client',
    install: 'cargo add zeep-orbit-client',
    snippet: `use zeep_orbit_client::{OrbitClient, ClientConfig};

let orbit = OrbitClient::new(ClientConfig {
    base_url: "https://orbit.zeeplabs.com".into(),
    app: "my_app".into(),
    jwt: "token".into(),
});

let rows = orbit.table("invoices")
    .find_many(Some(10), None, None, None).await?;`,
  },
  {
    name: 'Java',
    pkg: 'com.zeeplabs:orbit-client',
    install: `<!-- pom.xml -->
<dependency>
    <groupId>com.zeeplabs</groupId>
    <artifactId>orbit-client</artifactId>
    <version>0.1.0</version>
</dependency>`,
    snippet: `OrbitClient orbit = new OrbitClient(
    new ClientConfig(baseURL, "my_app", "token"));

ListResponse resp = orbit
    .table("invoices")
    .findMany(10, 0, null, null);`,
  },
  {
    name: 'PHP',
    pkg: 'zeeplabs/orbit-client',
    install: 'composer require zeeplabs/orbit-client',
    snippet: `$orbit = new Zeeplabs\\Orbit\\OrbitClient(
    baseURL: 'https://orbit.zeeplabs.com',
    app: 'my_app',
    jwt: 'token',
);

$rows = $orbit->table('invoices')->findMany(limit: 10);`,
  },
]

function CopyButton({ value, label }: { value: string; label: string }) {
  const [copied, setCopied] = useState(false)

  async function handleCopy() {
    try {
      await navigator.clipboard.writeText(value)
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    } catch {
      // Clipboard API pode falhar em iframes sem permissão; silencioso.
    }
  }

  return (
    <button
      type="button"
      onClick={handleCopy}
      title={label}
      aria-label={label}
      className="flex h-7 w-7 shrink-0 items-center justify-center rounded-md transition-colors"
      style={{ color: 'var(--text-tertiary)' }}
    >
      <Icon name={copied ? 'check' : 'content_copy'} size={15} />
    </button>
  )
}

/**
 * Tela de SDK clients (T2.8 do spec dashboard-redesign).
 * Handoff §F3-10: grid 2-col, cards com nome + pkg + install + code snippet
 * com syntax highlighting, ícone de copy em cada bloco.
 * Drop framer-motion + lucide-react (icons agora via <Icon>), tokens canônicos.
 */
export default function SdkPage() {
  const { t } = useTranslation()

  return (
    <>
      <PageHeader title={t('sdk.title')} subtitle={t('sdk.subtitle')} />

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
        {sdks.map((sdk) => (
          <div
            key={sdk.name}
            className="flex flex-col gap-3 rounded-[14px] border p-5"
            style={{ background: 'var(--surface)', borderColor: 'var(--border)' }}
          >
            <div className="flex items-center gap-2.5">
              <div
                className="flex h-[34px] w-[34px] items-center justify-center rounded-[9px]"
                style={{ background: 'var(--primary-tint)' }}
              >
                <Icon name="code" size={18} style={{ color: 'var(--primary)' }} />
              </div>
              <div className="min-w-0">
                <div
                  className="truncate text-[14.5px] font-bold"
                  style={{ color: 'var(--text-primary)', fontFamily: 'var(--font-display)' }}
                >
                  {sdk.name}
                </div>
                <div
                  className="truncate text-[11.5px]"
                  style={{ color: 'var(--text-tertiary)', fontFamily: 'var(--font-mono)' }}
                >
                  {sdk.pkg}
                </div>
              </div>
            </div>

            <div
              className="flex items-center gap-2 rounded-[10px] px-3 py-2"
              style={{ background: 'var(--bg-sunken)' }}
            >
              <span
                className={cn('flex-1 truncate text-[11.5px]')}
                style={{ color: 'var(--text-secondary)', fontFamily: 'var(--font-mono)' }}
              >
                {sdk.install}
              </span>
              <CopyButton value={sdk.install} label={t('sdk.copyInstall')} />
            </div>

            <div className="relative">
              <pre
                className="m-0 overflow-x-auto rounded-[10px] border p-3 text-[11px] leading-[1.5]"
                style={{
                  background: 'var(--bg-page)',
                  borderColor: 'var(--border)',
                  color: 'var(--text-secondary)',
                  fontFamily: 'var(--font-mono)',
                }}
              >
                <code>{sdk.snippet}</code>
              </pre>
              <div className="absolute right-2 top-2">
                <CopyButton value={sdk.snippet} label={t('sdk.copySnippet')} />
              </div>
            </div>
          </div>
        ))}
      </div>
    </>
  )
}
