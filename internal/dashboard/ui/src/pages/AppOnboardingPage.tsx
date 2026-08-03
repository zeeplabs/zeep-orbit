import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { useCreateApp } from "../lib/api";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Button } from "@/components/ui/button";
import { Icon } from "@/components/ui/icon";
import { PageHeader, SettingRow } from "@/components/patterns";

export default function AppOnboardingPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const createApp = useCreateApp();

  const [appName, setAppName] = useState("");
  const [authEmail, setAuthEmail] = useState(false);
  const [nameError, setNameError] = useState<string | null>(null);
  const [submitError, setSubmitError] = useState<string | null>(null);

  function validateName(name: string): string | null {
    if (!name.trim()) return t("appOnboarding.errNameRequired");
    if (!/^[a-z][a-z0-9_-]*$/.test(name)) return t("appOnboarding.errNameFormat");
    if (name.length > 32) return t("appOnboarding.errNameMax");
    return null;
  }

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    const err = validateName(appName);
    setNameError(err);
    if (err) return;

    setSubmitError(null);
    try {
      const app = await createApp.mutateAsync({ name: appName, auth_email_enabled: authEmail });
      navigate(`/apps/${app.id}`);
    } catch (err) {
      setSubmitError(err instanceof Error ? err.message : t("appOnboarding.errUnexpected"));
    }
  }

  return (
    <div>
      <button
        type="button"
        onClick={() => navigate("/apps")}
        className="mb-5 flex cursor-pointer items-center gap-1.5 border-none bg-transparent text-[13px] text-[var(--text-secondary)] transition-colors hover:text-[var(--text-primary)]"
      >
        <Icon name="arrow_back" size={16} />
        {t("appOnboarding.back")}
      </button>

      <PageHeader title={t("appOnboarding.title")} subtitle={t("appOnboarding.subtitle")} />

      <form onSubmit={handleSubmit} className="flex flex-col gap-6">
        <div className="flex flex-col gap-5 rounded-2xl border border-[var(--border)] bg-[var(--surface)] p-5">
          <div className="flex flex-col gap-1.5">
            <Label>{t("appOnboarding.nameLabel")}</Label>
            <Input
              value={appName}
              onChange={(e) => setAppName(e.target.value.toLowerCase().replace(/[\s]+/g, "-"))}
              placeholder={t("appOnboarding.namePlaceholder")}
              className={nameError ? "border-[var(--danger)]" : undefined}
            />
            {nameError && <p className="text-xs text-[var(--danger)]">{nameError}</p>}
            <p className="text-[11px] text-[var(--text-tertiary)]">{t("appOnboarding.nameHint")}</p>
          </div>

          <div className="border-t border-[var(--border)]" />

          <SettingRow
            label={t("appOnboarding.authEmailTitle")}
            description={t("appOnboarding.authEmailDesc")}
            control={<Switch checked={authEmail} onCheckedChange={setAuthEmail} />}
            className="py-0"
          />
        </div>

        {submitError && (
          <div className="rounded-2xl border border-[var(--danger)]/20 bg-[var(--danger-tint)] px-6 py-4 text-sm text-[var(--danger)]">
            {submitError}
          </div>
        )}

        <div className="flex items-center gap-3">
          <Button type="submit" disabled={createApp.isPending}>
            {createApp.isPending ? t("appOnboarding.creating") : t("appOnboarding.submit")}
          </Button>
          <Button type="button" variant="outline" onClick={() => navigate("/apps")}>
            {t("appOnboarding.cancel")}
          </Button>
        </div>
      </form>
    </div>
  );
}
