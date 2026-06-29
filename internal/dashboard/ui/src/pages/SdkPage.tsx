import { motion } from "framer-motion";
import { useTranslation } from "react-i18next";
import { Code2, Copy, Check } from "lucide-react";
import { useState } from "react";

const ease = [0.32, 0.72, 0, 1] as const;

const fadeUp = {
  initial: { opacity: 0, y: 16 },
  animate: { opacity: 1, y: 0 },
  transition: { duration: 0.6, ease },
};

const sdks = [
  {
    name: "TypeScript",
    pkg: "@zeeptech/orbit-client",
    install: "npm install @zeeptech/orbit-client",
    import_code: `import { OrbitClient } from '@zeeptech/orbit-client'

const orbit = new OrbitClient({
  baseURL: 'https://orbit.zeeplabs.com',
  app: 'my_app',
  jwt: 'token',
})

const rows = await orbit.table('invoices').findMany({ limit: 10 })`,
  },
  {
    name: "Go",
    pkg: "github.com/zeeplabs/orbit-go",
    install: "go get github.com/zeeplabs/orbit-go",
    import_code: `import "github.com/zeeplabs/orbit-go"

client := orbit.New(orbit.ClientConfig{
    BaseURL: "https://orbit.zeeplabs.com",
    App:     "my_app",
    JWT:     "token",
})

rows, _ := client.Table("invoices").FindMany(ctx, &orbit.FindManyParams{Limit: 10})`,
  },
  {
    name: "Python",
    pkg: "zeeplabs-orbit-client",
    install: "pip install zeeplabs-orbit-client",
    import_code: `from zeeplabs_orbit_client import OrbitClient, ClientConfig

orbit = OrbitClient(ClientConfig(
    base_url="https://orbit.zeeplabs.com",
    app="my_app",
    jwt="token",
))

rows = orbit.table("invoices").find_many(limit=10)`,
  },
  {
    name: "Rust",
    pkg: "zeep-orbit-client",
    install: "cargo add zeep-orbit-client",
    import_code: `use zeep_orbit_client::{OrbitClient, ClientConfig};

let orbit = OrbitClient::new(ClientConfig {
    base_url: "https://orbit.zeeplabs.com".into(),
    app: "my_app".into(),
    jwt: "token".into(),
});

let rows = orbit.table("invoices")
    .find_many(Some(10), None, None, None).await?;`,
  },
  {
    name: "Java",
    pkg: "com.zeeplabs:orbit-client",
    install: `<!-- pom.xml -->
<dependency>
    <groupId>com.zeeplabs</groupId>
    <artifactId>orbit-client</artifactId>
    <version>0.1.0</version>
</dependency>`,
    import_code: `OrbitClient orbit = new OrbitClient(
    new ClientConfig(baseURL, "my_app", "token"));

ListResponse resp = orbit
    .table("invoices")
    .findMany(10, 0, null, null);`,
  },
  {
    name: "PHP",
    pkg: "zeeplabs/orbit-client",
    install: "composer require zeeplabs/orbit-client",
    import_code: `$orbit = new Zeeplabs\\Orbit\\OrbitClient(
    baseURL: 'https://orbit.zeeplabs.com',
    app: 'my_app',
    jwt: 'token',
);

$rows = $orbit->table('invoices')->findMany(limit: 10);`,
  },
];

function CodeBlock({ code, language }: { code: string; language: string }) {
  const [copied, setCopied] = useState(false);

  const handleCopy = () => {
    navigator.clipboard.writeText(code);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  return (
    <div className="relative group">
      <div className="flex items-center justify-between px-4 py-2 rounded-t-xl border-x border-t border-white/[0.08] bg-white/[0.04]">
        <span className="text-[11px] font-semibold uppercase tracking-wider text-[#64748B]">{language}</span>
        <button
          onClick={handleCopy}
          className="flex items-center gap-1.5 text-[11px] text-[#64748B] hover:text-[#F8FAFC] bg-transparent border-none cursor-pointer transition-colors"
        >
          {copied ? <Check size={12} /> : <Copy size={12} />}
          {copied ? "Copied!" : "Copy"}
        </button>
      </div>
      <pre className="m-0 p-4 rounded-b-xl border border-white/[0.08] bg-[#0A0A0F] overflow-x-auto text-[12px] leading-relaxed font-mono text-[#A78BFA]">
        <code>{code}</code>
      </pre>
    </div>
  );
}

export default function SdkPage() {
  const { t } = useTranslation();

  return (
    <div className="relative z-10">
      <motion.div {...fadeUp} className="mb-8 max-md:mb-6">
        <span
          className="mb-3 inline-flex items-center gap-1.5 rounded-full border px-3 py-1 text-[10px] font-bold uppercase tracking-[0.12em]"
          style={{
            borderColor: "rgba(var(--brand-primary-rgb), 0.2)",
            backgroundColor: "rgba(var(--brand-primary-rgb), 0.12)",
            color: "var(--brand-light)",
          }}
        >
          <Code2 size={12} strokeWidth={1.5} />
          SDKs
        </span>

        <div className="flex flex-wrap items-end justify-between gap-4">
          <div>
            <h2 className="mb-1.5 text-[28px] max-md:text-[22px] font-extrabold leading-tight">
              SDK Clients
            </h2>
            <p className="text-sm max-md:text-[13px] text-[#94A3B8]">
              Official clients for all major languages. Same API design across all.
            </p>
          </div>
        </div>
      </motion.div>

      <div className="grid grid-cols-1 gap-6">
        {sdks.map((sdk, i) => (
          <motion.div
            key={sdk.name}
            initial={{ opacity: 0, y: 16 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ duration: 0.5, ease, delay: i * 0.08 }}
            className="rounded-2xl border border-white/[0.06] bg-white/[0.03] p-5"
          >
            <div className="flex items-center justify-between mb-4">
              <div>
                <h3 className="text-[16px] font-bold text-[#F8FAFC]">{sdk.name}</h3>
                <code className="text-[12px] text-[#94A3B8] font-mono">{sdk.pkg}</code>
              </div>
              <code className="text-[11px] px-3 py-1.5 rounded-lg border border-white/[0.08] bg-white/[0.04] text-[#94A3B8] font-mono">
                {sdk.install}
              </code>
            </div>
            <CodeBlock code={sdk.import_code} language={sdk.name} />
          </motion.div>
        ))}
      </div>
    </div>
  );
}
