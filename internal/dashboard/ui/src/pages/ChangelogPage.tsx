import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useQuery } from "@tanstack/react-query";
import { Megaphone } from "lucide-react";
import { Button } from "@/components/ui/button";

const PAGE_SIZE = 10;

type SectionType = "features" | "improvements" | "fixes" | "security" | "breaking";

interface SectionItem {
  description: string;
}

interface Section {
  type: SectionType;
  items: SectionItem[];
}

interface ChangelogEntry {
  id?: string;
  version: string;
  release_date: string;
  title: string;
  summary: string;
  sections: Section[];
}

function parseSections(sections: unknown): Section[] {
  if (Array.isArray(sections)) return sections;
  if (typeof sections === "string") {
    try { return JSON.parse(sections); } catch { return []; }
  }
  return [];
}

const sectionColors: Record<SectionType, { bg: string; text: string; border: string }> = {
  features: { bg: "bg-emerald-500/10", text: "text-emerald-400", border: "border-emerald-500/20" },
  improvements: { bg: "bg-blue-500/10", text: "text-blue-400", border: "border-blue-500/20" },
  fixes: { bg: "bg-amber-500/10", text: "text-amber-400", border: "border-amber-500/20" },
  security: { bg: "bg-red-500/10", text: "text-red-400", border: "border-red-500/20" },
  breaking: { bg: "bg-purple-500/10", text: "text-purple-400", border: "border-purple-500/20" },
};

function formatDate(dateStr: string): string {
  const d = new Date(dateStr + "T00:00:00");
  return d.toLocaleDateString("pt-BR", { day: "2-digit", month: "short", year: "numeric" });
}

function ChangelogEntryView({ entry }: { entry: ChangelogEntry }) {
  const { t } = useTranslation();
  const sections = parseSections(entry.sections);

  return (
    <div className="relative border border-white/[0.06] rounded-xl bg-white/[0.02] p-6">
      <div className="flex items-center gap-3 mb-4">
        <span className="px-2.5 py-0.5 rounded-md bg-white/[0.06] text-xs font-mono text-[#F8FAFC]">
          {entry.version}
        </span>
        <span className="text-xs text-[#94A3B8]">{formatDate(entry.release_date)}</span>
      </div>

      {entry.title && (
        <h3 className="text-sm font-semibold text-[#F8FAFC] mb-1">{entry.title}</h3>
      )}
      {entry.summary && (
        <p className="text-[13px] text-[#94A3B8] mb-4">{entry.summary}</p>
      )}

      <div className="space-y-4">
        {sections.map((section, si) => {
          const colors = sectionColors[section.type] || sectionColors.fixes;
          return (
            <div key={si}>
              <span
                className={`inline-block px-2 py-0.5 rounded text-[11px] font-semibold ${colors.bg} ${colors.text} border ${colors.border} mb-2`}
              >
                {t(`changelog.sectionType.${section.type}` as any)}
              </span>
              <ul className="space-y-1.5">
                {section.items.map((item, ii) => (
                  <li key={ii} className="flex items-start gap-2 text-[13px] text-[#94A3B8]">
                    <span className="text-white/20 mt-1.5 block w-1 h-1 rounded-full bg-current flex-shrink-0" />
                    {item.description}
                  </li>
                ))}
              </ul>
            </div>
          );
        })}
      </div>
    </div>
  );
}

export default function ChangelogPage() {
  const { t } = useTranslation();
  const [offset, setOffset] = useState(0);

  const { data, isLoading } = useQuery({
    queryKey: ["changelog"],
    queryFn: async () => {
      const res = await fetch("/dashboard/api/changelog", { credentials: "include" });
      if (!res.ok) throw new Error("Failed to load");
      return res.json() as Promise<{ entries: ChangelogEntry[] }>;
    },
    staleTime: 300000,
  });

  const allEntries = data?.entries || [];
  const entries = allEntries.slice(0, offset + PAGE_SIZE);
  const hasMore = offset + PAGE_SIZE < allEntries.length;

  return (
    <div className="w-full max-w-2xl mx-auto py-2">
      <div className="mb-8">
        <h1 className="text-xl font-bold text-[#F8FAFC]">{t("changelog.title")}</h1>
      </div>

      {isLoading ? (
        <div className="text-center text-[#94A3B8] py-12">Carregando...</div>
      ) : entries.length === 0 ? (
        <div className="text-center py-16">
          <Megaphone size={40} strokeWidth={1} className="mx-auto text-white/10 mb-4" />
          <p className="text-[#94A3B8] text-sm">{t("changelog.empty")}</p>
        </div>
      ) : (
        <>
          <div className="space-y-4">
            {entries.map((entry, i) => (
              <ChangelogEntryView key={entry.id || i} entry={entry} />
            ))}
          </div>

          {hasMore && (
            <div className="text-center mt-6">
              <Button
                variant="outline"
                onClick={() => setOffset((p) => p + PAGE_SIZE)}
                className="rounded-xl border-white/[0.10] bg-white/[0.04] text-[#94A3B8] hover:bg-white/[0.08] hover:text-[#F8FAFC]"
              >
                {t("changelog.loadMore")}
              </Button>
            </div>
          )}
        </>
      )}
    </div>
  );
}
