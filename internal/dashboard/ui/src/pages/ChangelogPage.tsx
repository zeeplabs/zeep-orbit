import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { Megaphone, Pencil, Trash2, Plus, X } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import DeleteConfirmDialog from "@/components/DeleteConfirmDialog";

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
  id: string;
  version: string;
  release_date: string;
  title: string;
  summary: string;
  sections: string;
  published: boolean;
  created_at: string;
}

function parseSections(raw: string): Section[] {
  try {
    return JSON.parse(raw);
  } catch {
    return [];
  }
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

function ChangelogEntryView({
  entry,
  isSuperadmin,
  onEdit,
  onDelete,
}: {
  entry: ChangelogEntry;
  isSuperadmin: boolean;
  onEdit: () => void;
  onDelete: () => void;
}) {
  const { t } = useTranslation();
  const sections = parseSections(entry.sections);

  return (
    <div className="relative border border-white/[0.06] rounded-xl bg-white/[0.02] p-6">
      {isSuperadmin && (
        <div className="absolute top-4 right-4 flex gap-1">
          <button
            onClick={onEdit}
            className="p-1.5 rounded-lg hover:bg-white/[0.08] text-white/40 hover:text-white/70 transition-colors"
          >
            <Pencil size={14} />
          </button>
          <button
            onClick={onDelete}
            className="p-1.5 rounded-lg hover:bg-red-500/10 text-white/40 hover:text-red-400 transition-colors"
          >
            <Trash2 size={14} />
          </button>
        </div>
      )}

      <div className="flex items-center gap-3 mb-4">
        <span className="px-2.5 py-0.5 rounded-md bg-white/[0.06] text-xs font-mono text-[#F8FAFC]">
          {entry.version}
        </span>
        <span className="text-xs text-[#94A3B8]">{formatDate(entry.release_date)}</span>
        {!entry.published && (
          <span className="text-[10px] px-1.5 py-0.5 rounded bg-amber-500/10 text-amber-400">
            Draft
          </span>
        )}
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

interface ChangelogFormData {
  id?: string;
  version: string;
  release_date: string;
  title: string;
  summary: string;
  sections: Section[];
  published: boolean;
}

function emptySection(): Section {
  return { type: "features", items: [{ description: "" }] };
}

function ChangelogFormModal({
  open,
  onClose,
  initial,
}: {
  open: boolean;
  onClose: () => void;
  initial?: ChangelogEntry;
}) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const isEdit = !!initial;

  const [form, setForm] = useState<ChangelogFormData>(() => {
    if (initial) {
      return {
        id: initial.id,
        version: initial.version,
        release_date: initial.release_date,
        title: initial.title,
        summary: initial.summary,
        sections: parseSections(initial.sections),
        published: initial.published,
      };
    }
    return {
      version: "",
      release_date: new Date().toISOString().slice(0, 10),
      title: "",
      summary: "",
      sections: [emptySection()],
      published: true,
    };
  });

  const mutation = useMutation({
    mutationFn: async () => {
      const method = isEdit ? "PUT" : "POST";
      const url = isEdit ? `/dashboard/api/changelog/${form.id}` : "/dashboard/api/changelog";
      const res = await fetch(url, {
        method,
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          version: form.version,
          release_date: form.release_date,
          title: form.title,
          summary: form.summary,
          sections: JSON.stringify(form.sections),
          published: form.published,
        }),
      });
      if (!res.ok) {
        const data = await res.json().catch(() => ({}));
        throw new Error((data as any).error || "Failed to save");
      }
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["changelog"] });
      toast.success(isEdit ? "Entrada atualizada!" : "Entrada criada!");
      onClose();
    },
    onError: (err: Error) => {
      toast.error(err.message);
    },
  });

  const updateSection = (si: number, field: keyof Section, value: string) => {
    setForm((prev) => {
      const next = { ...prev, sections: [...prev.sections] };
      if (field === "type") {
        next.sections[si] = { ...next.sections[si], type: value as SectionType };
      }
      return next;
    });
  };

  const updateItem = (si: number, ii: number, value: string) => {
    setForm((prev) => {
      const next = { ...prev, sections: [...prev.sections] };
      const items = [...next.sections[si].items];
      items[ii] = { description: value };
      next.sections[si] = { ...next.sections[si], items };
      return next;
    });
  };

  const addSection = () => {
    setForm((prev) => ({
      ...prev,
      sections: [...prev.sections, emptySection()],
    }));
  };

  const removeSection = (si: number) => {
    setForm((prev) => ({
      ...prev,
      sections: prev.sections.filter((_, i) => i !== si),
    }));
  };

  const addItem = (si: number) => {
    setForm((prev) => {
      const next = { ...prev, sections: [...prev.sections] };
      next.sections[si] = {
        ...next.sections[si],
        items: [...next.sections[si].items, { description: "" }],
      };
      return next;
    });
  };

  const removeItem = (si: number, ii: number) => {
    setForm((prev) => {
      const next = { ...prev, sections: [...prev.sections] };
      next.sections[si] = {
        ...next.sections[si],
        items: next.sections[si].items.filter((_, i) => i !== ii),
      };
      return next;
    });
  };

  return (
    <Dialog open={open} onOpenChange={(o) => { if (!o) onClose(); }}>
      <DialogContent className="max-w-xl max-h-[85vh] overflow-y-auto border border-white/[0.10] bg-[#0D0D14]/60 backdrop-blur-xl rounded-2xl p-0 gap-0">
        <div className="bg-white/[0.04] shadow-[inset_0_1px_1px_rgba(255,255,255,0.10)] rounded-[calc(1rem-2px)] px-7 pb-6 pt-7">
          <DialogHeader className="mb-4">
            <DialogTitle className="text-base font-bold text-[#F8FAFC]">
              {isEdit ? t("changelog.editTitle") : t("changelog.createTitle")}
            </DialogTitle>
            <DialogDescription className="text-[13px] text-[#94A3B8] mt-1" />
          </DialogHeader>

          <div className="space-y-3">
            <div>
              <label className="text-[11px] font-semibold text-[#94A3B8] uppercase tracking-wide">
                {t("changelog.version")}
              </label>
              <Input
                value={form.version}
                onChange={(e) => setForm((p) => ({ ...p, version: e.target.value }))}
                placeholder="1.0.0"
                className="mt-1"
              />
            </div>
            <div>
              <label className="text-[11px] font-semibold text-[#94A3B8] uppercase tracking-wide">
                {t("changelog.date")}
              </label>
              <Input
                type="date"
                value={form.release_date}
                onChange={(e) => setForm((p) => ({ ...p, release_date: e.target.value }))}
                className="mt-1"
              />
            </div>
            <div>
              <label className="text-[11px] font-semibold text-[#94A3B8] uppercase tracking-wide">
                {t("changelog.titleField")}
              </label>
              <Input
                value={form.title}
                onChange={(e) => setForm((p) => ({ ...p, title: e.target.value }))}
                placeholder="Initial Release"
                className="mt-1"
              />
            </div>
            <div>
              <label className="text-[11px] font-semibold text-[#94A3B8] uppercase tracking-wide">
                {t("changelog.summaryField")}
              </label>
              <Input
                value={form.summary}
                onChange={(e) => setForm((p) => ({ ...p, summary: e.target.value }))}
                placeholder="Summary..."
                className="mt-1"
              />
            </div>

            <div>
              <div className="flex items-center justify-between mb-2">
                <label className="text-[11px] font-semibold text-[#94A3B8] uppercase tracking-wide">
                  Sections
                </label>
                <button
                  type="button"
                  onClick={addSection}
                  className="text-[11px] text-[--brand-primary] hover:underline"
                >
                  + {t("changelog.addSection")}
                </button>
              </div>
              <div className="space-y-3">
                {form.sections.map((section, si) => (
                  <div key={si} className="p-3 rounded-lg border border-white/[0.06] bg-white/[0.02]">
                    <div className="flex items-center gap-2 mb-2">
                      <select
                        value={section.type}
                        onChange={(e) => updateSection(si, "type", e.target.value)}
                        className="flex-1 rounded-lg border border-white/[0.10] bg-white/[0.04] text-[#F8FAFC] text-xs px-2 py-1.5"
                      >
                        <option value="features">{t("changelog.sectionType.features" as any)}</option>
                        <option value="improvements">{t("changelog.sectionType.improvements" as any)}</option>
                        <option value="fixes">{t("changelog.sectionType.fixes" as any)}</option>
                        <option value="security">{t("changelog.sectionType.security" as any)}</option>
                        <option value="breaking">{t("changelog.sectionType.breaking" as any)}</option>
                      </select>
                      <button
                        type="button"
                        onClick={() => removeSection(si)}
                        className="p-1 rounded text-white/30 hover:text-red-400"
                      >
                        <X size={14} />
                      </button>
                    </div>
                    <div className="space-y-1.5">
                      {section.items.map((item, ii) => (
                        <div key={ii} className="flex items-center gap-1.5">
                          <Input
                            value={item.description}
                            onChange={(e) => updateItem(si, ii, e.target.value)}
                            placeholder={t("changelog.itemDesc")}
                            className="flex-1 h-8 text-xs"
                          />
                          <button
                            type="button"
                            onClick={() => removeItem(si, ii)}
                            className="p-1 rounded text-white/30 hover:text-red-400 flex-shrink-0"
                          >
                            <X size={12} />
                          </button>
                        </div>
                      ))}
                    </div>
                    <button
                      type="button"
                      onClick={() => addItem(si)}
                      className="mt-2 text-[10px] text-white/40 hover:text-white/70"
                    >
                      + {t("changelog.addItem")}
                    </button>
                  </div>
                ))}
              </div>
            </div>
          </div>

          <DialogFooter className="flex flex-row gap-2.5 sm:flex-row sm:justify-end sm:space-x-0 mt-4">
            <Button
              variant="outline"
              onClick={onClose}
              className="rounded-xl border-white/[0.10] bg-white/[0.06] text-[#94A3B8] hover:bg-white/[0.10]"
            >
              {t("changelog.cancel")}
            </Button>
            <Button
              onClick={() => mutation.mutate()}
              disabled={mutation.isPending || !form.version}
              className="rounded-xl border-0 text-white font-semibold disabled:opacity-40"
              style={{
                background: "linear-gradient(to bottom right, var(--brand-primary), var(--brand-secondary))",
              }}
            >
              {mutation.isPending ? "..." : isEdit ? t("changelog.save") : t("changelog.create")}
            </Button>
          </DialogFooter>
        </div>
      </DialogContent>
    </Dialog>
  );
}

export default function ChangelogPage() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [offset, setOffset] = useState(0);
  const [showForm, setShowForm] = useState(false);
  const [editingEntry, setEditingEntry] = useState<ChangelogEntry | undefined>();
  const [deletingEntry, setDeletingEntry] = useState<ChangelogEntry | undefined>();

  const { data: me } = useQuery({
    queryKey: ["me"],
    queryFn: async () => {
      const res = await fetch("/dashboard/api/me", { credentials: "include" });
      return res.json();
    },
    staleTime: 60000,
  });

  const { data, isLoading } = useQuery({
    queryKey: ["changelog", offset],
    queryFn: async () => {
      const res = await fetch(
        `/dashboard/api/changelog?limit=${PAGE_SIZE}&offset=${offset}`,
        { credentials: "include" }
      );
      if (!res.ok) throw new Error("Failed to load");
      return res.json() as Promise<{ entries: ChangelogEntry[]; total: number }>;
    },
  });

  const deleteMutation = useMutation({
    mutationFn: async (id: string) => {
      const res = await fetch(`/dashboard/api/changelog/${id}`, {
        method: "DELETE",
        credentials: "include",
      });
      if (!res.ok) throw new Error("Failed to delete");
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["changelog"] });
      toast.success("Entrada removida!");
      setDeletingEntry(undefined);
    },
    onError: () => toast.error("Erro ao remover entrada"),
  });

  const isSuperadmin = me?.role === "superadmin";
  const entries = data?.entries || [];
  const total = data?.total || 0;
  const hasMore = offset + PAGE_SIZE < total;

  return (
    <div className="w-full max-w-2xl mx-auto py-2">
      <div className="flex items-center justify-between mb-8">
        <div>
          <h1 className="text-xl font-bold text-[#F8FAFC]">{t("changelog.title")}</h1>
        </div>
        {isSuperadmin && (
          <Button
            onClick={() => {
              setEditingEntry(undefined);
              setShowForm(true);
            }}
            className="rounded-xl border-0 text-white font-semibold"
            style={{
              background: "linear-gradient(to bottom right, var(--brand-primary), var(--brand-secondary))",
            }}
            size="sm"
          >
            <Plus size={14} strokeWidth={2} className="mr-1.5" />
            {t("changelog.add")}
          </Button>
        )}
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
            {entries.map((entry) => (
              <ChangelogEntryView
                key={entry.id}
                entry={entry}
                isSuperadmin={isSuperadmin}
                onEdit={() => {
                  setEditingEntry(entry);
                  setShowForm(true);
                }}
                onDelete={() => setDeletingEntry(entry)}
              />
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

      <ChangelogFormModal
        open={showForm}
        onClose={() => {
          setShowForm(false);
          setEditingEntry(undefined);
        }}
        initial={editingEntry}
      />

      <DeleteConfirmDialog
        open={!!deletingEntry}
        appName=""
        onCancel={() => setDeletingEntry(undefined)}
        onConfirm={() => deletingEntry && deleteMutation.mutate(deletingEntry.id)}
        titleKey="changelog.deleteConfirm"
        descKey="changelog.deleteDesc"
        loading={deleteMutation.isPending}
      />
    </div>
  );
}
