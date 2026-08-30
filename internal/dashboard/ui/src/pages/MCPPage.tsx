import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  PersonalAccessToken,
  RegisteredOAuthClient,
  useCreatePAT,
  useDeleteOAuthClient,
  useOAuthClients,
  usePATs,
  useRevokePAT,
} from "../lib/api";
import { PageHeader } from "@/components/patterns";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Icon } from "@/components/ui/icon";
import { EmptyState, LoadingState } from "@/components/patterns/states";
import { ConfirmDialog } from "@/components/patterns/ConfirmDialog";
import { highlight, type SdkLang } from "@/lib/prism";

function endpointUrl(): string {
  const base = typeof window !== "undefined" ? window.location.origin : "";
  return `${base}/dashboard/mcp`;
}

async function copyToClipboard(value: string, successMessage: string) {
  try {
    await navigator.clipboard.writeText(value);
    toast.success(successMessage);
  } catch {
    // Clipboard API can be unavailable (permissions, non-HTTPS context) --
    // the value is still visible on screen for manual copy.
  }
}

function CopyButton({
  value,
  label,
  successMessage,
}: {
  value: string;
  label: string;
  successMessage: string;
}) {
  return (
    <Button
      variant="outline"
      size="icon"
      className="size-8 shrink-0"
      title={label}
      aria-label={label}
      onClick={() => copyToClipboard(value, successMessage)}
    >
      <Icon name="content_copy" size={15} />
    </Button>
  );
}

interface ClientEntry {
  name: string;
  file: string;
  lang: SdkLang;
  snippet: (endpoint: string) => string;
}

const CLIENTS: ClientEntry[] = [
  {
    name: "Claude Code",
    file: ".mcp.json",
    lang: "json",
    snippet: (endpoint) => `{
  "mcpServers": {
    "zeep-orbit": {
      "type": "http",
      "url": "${endpoint}",
      "headers": {
        "Authorization": "Bearer \${ZEEP_ORBIT_PAT}"
      }
    }
  }
}`,
  },
  {
    name: "Codex",
    file: "~/.codex/config.toml",
    lang: "toml",
    snippet: (endpoint) => `[mcp_servers.zeep-orbit]
url = "${endpoint}"
bearer_token_env_var = "ZEEP_ORBIT_PAT"`,
  },
  {
    name: "Cursor",
    file: ".cursor/mcp.json",
    lang: "json",
    snippet: (endpoint) => `{
  "mcpServers": {
    "zeep-orbit": {
      "url": "${endpoint}",
      "headers": {
        "Authorization": "Bearer \${ZEEP_ORBIT_PAT}"
      }
    }
  }
}`,
  },
  {
    name: "OpenCode",
    file: "opencode.json",
    lang: "json",
    snippet: (endpoint) => `{
  "mcp": {
    "zeep-orbit": {
      "type": "remote",
      "url": "${endpoint}",
      "headers": {
        "Authorization": "Bearer \${ZEEP_ORBIT_PAT}"
      },
      "enabled": true
    }
  }
}`,
  },
];

function EndpointBlock({ t }: { t: (key: string) => string }) {
  const endpoint = endpointUrl();
  return (
    <div className="flex flex-col gap-2">
      <Label>{t("mcp.endpointLabel")}</Label>
      <div className="flex items-center gap-2 rounded-[10px] border border-[var(--border)] bg-[var(--sunken)] px-4 py-3">
        <code
          data-testid="mcp-endpoint-url"
          className="flex-1 overflow-x-auto break-all font-mono text-sm text-[var(--primary)]"
        >
          {endpoint}
        </code>
        <CopyButton value={endpoint} label={t("mcp.copyEndpoint")} successMessage={t("mcp.copySuccess")} />
      </div>
    </div>
  );
}

function AuthExplainer({ t }: { t: (key: string) => string }) {
  return (
    <div className="flex flex-col gap-3">
      <h3 className="text-[13px] font-semibold text-[var(--text-primary)]">{t("mcp.authTitle")}</h3>
      <div className="flex flex-col gap-3 sm:flex-row">
        <div className="flex-1 rounded-[10px] border border-[var(--border)] p-4">
          <p className="text-sm font-semibold text-[var(--text-primary)]">{t("mcp.authPatTitle")}</p>
          <p className="mt-1 text-[12.5px] text-[var(--text-secondary)]">{t("mcp.authPatDesc")}</p>
        </div>
        <div className="flex-1 rounded-[10px] border border-[var(--border)] p-4">
          <p className="text-sm font-semibold text-[var(--text-primary)]">{t("mcp.authOAuthTitle")}</p>
          <p className="mt-1 text-[12.5px] text-[var(--text-secondary)]">{t("mcp.authOAuthDesc")}</p>
        </div>
      </div>
    </div>
  );
}

function ClientTutorials({ t }: { t: (key: string) => string }) {
  const endpoint = endpointUrl();
  return (
    <div className="flex flex-col gap-3">
      <div>
        <h3 className="text-[13px] font-semibold text-[var(--text-primary)]">{t("mcp.clientsTitle")}</h3>
        <p className="mt-1 text-[12.5px] text-[var(--text-secondary)]">{t("mcp.clientsSubtitle")}</p>
      </div>
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
        {CLIENTS.map((client) => {
          const snippet = client.snippet(endpoint);
          return (
            <div
              key={client.name}
              data-testid={`mcp-client-card-${client.name}`}
              className="flex flex-col gap-2 rounded-[14px] border p-4"
              style={{ background: "var(--surface)", borderColor: "var(--border)" }}
            >
              <div className="flex items-center justify-between gap-2">
                <div className="min-w-0">
                  <p className="truncate text-[13.5px] font-bold text-[var(--text-primary)]">{client.name}</p>
                  <p className="truncate font-mono text-[11px] text-[var(--text-tertiary)]">{client.file}</p>
                </div>
              </div>
              <div className="relative">
                <pre
                  data-testid={`mcp-client-snippet-${client.name}`}
                  data-snippet={snippet}
                  className="m-0 overflow-x-auto rounded-[10px] border p-3 text-[11px] leading-[1.5]"
                  style={{
                    background: "var(--bg-sunken)",
                    borderColor: "var(--border)",
                    color: "var(--text-secondary)",
                    fontFamily: "var(--font-mono)",
                  }}
                >
                  <code dangerouslySetInnerHTML={{ __html: highlight(snippet, client.lang) }} />
                </pre>
                <div className="absolute right-2 top-2">
                  <CopyButton value={snippet} label={t("mcp.copyConfig")} successMessage={t("mcp.copySuccess")} />
                </div>
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
}

function PersonalAccessTokensSection() {
  const { t } = useTranslation();
  const { data: allPats, isLoading } = usePATs();
  // ListPATs returns every token including revoked ones (design.md: kept
  // for auditability) -- filter to the active list so revoking one visibly
  // removes it here without a manual reload.
  const pats = allPats?.filter((pat) => !pat.revoked_at);
  const createPAT = useCreatePAT();
  const revokePAT = useRevokePAT();

  const [showCreateForm, setShowCreateForm] = useState(false);
  const [name, setName] = useState("");
  const [createdToken, setCreatedToken] = useState<string | null>(null);
  const [revokeTarget, setRevokeTarget] = useState<PersonalAccessToken | null>(null);

  const handleCreate = async () => {
    if (!name.trim()) return;
    try {
      const result = await createPAT.mutateAsync(name.trim());
      setCreatedToken(result.token);
      setShowCreateForm(false);
      setName("");
    } catch {
      // useCreatePAT's onError already shows toast.error(error.message)
    }
  };

  const confirmRevoke = () => {
    if (!revokeTarget) return;
    revokePAT.mutate(revokeTarget.id);
    setRevokeTarget(null);
  };

  return (
    <div className="flex flex-col gap-3">
      <div>
        <h3 className="text-[13px] font-semibold text-[var(--text-primary)]">{t("pats.title")}</h3>
        <p className="mt-1 text-[12.5px] text-[var(--text-secondary)]">{t("pats.explainer")}</p>
      </div>

      {createdToken ? (
        <div className="flex flex-col gap-3">
          <p className="text-[12px] font-semibold text-[var(--warning)]">{t("pats.createdWarning")}</p>
          <div className="flex items-center gap-2 rounded-[10px] border border-[var(--border)] bg-[var(--sunken)] px-4 py-3">
            <code
              data-testid="revealed-pat-token"
              className="max-h-32 flex-1 overflow-y-auto break-all font-mono text-sm text-[var(--primary)]"
            >
              {createdToken}
            </code>
            <CopyButton value={createdToken} label={t("common.copyToClipboard")} successMessage={t("pats.copySuccess")} />
          </div>
          <Button className="w-fit" onClick={() => setCreatedToken(null)}>{t("pats.done")}</Button>
        </div>
      ) : (
        <>
          {showCreateForm ? (
            <div className="flex max-w-sm flex-col gap-3">
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="pat-name">{t("pats.nameLabel")}</Label>
                <Input
                  id="pat-name"
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  placeholder={t("pats.namePlaceholder")}
                />
              </div>
              <div className="flex gap-2">
                <Button
                  variant="outline"
                  className="flex-1"
                  onClick={() => { setShowCreateForm(false); setName(""); }}
                  disabled={createPAT.isPending}
                >
                  {t("pats.cancel")}
                </Button>
                <Button className="flex-1" onClick={handleCreate} disabled={!name.trim() || createPAT.isPending}>
                  {createPAT.isPending ? t("pats.creating") : t("pats.create")}
                </Button>
              </div>
            </div>
          ) : (
            <Button className="w-fit gap-1.5" size="sm" onClick={() => setShowCreateForm(true)}>
              <Icon name="add" size={15} />
              {t("pats.newToken")}
            </Button>
          )}

          <div className="mt-1">
            {isLoading ? (
              <LoadingState rows={2} />
            ) : !pats?.length ? (
              <EmptyState icon="key" title={t("pats.empty")} description={t("pats.emptyDesc")} />
            ) : (
              <div className="flex flex-col gap-2">
                {pats.map((pat) => (
                  <div
                    key={pat.id}
                    data-testid={`pat-row-${pat.name}`}
                    className="flex items-center justify-between rounded-[10px] border border-[var(--border)] bg-[var(--sunken)] px-4 py-3"
                  >
                    <div className="flex min-w-0 flex-col gap-0.5">
                      <p className="truncate text-sm font-semibold text-[var(--text-primary)]">{pat.name}</p>
                      <p className="text-[11px] text-[var(--text-tertiary)]">
                        {pat.last_used_at
                          ? t("pats.lastUsed", { date: new Date(pat.last_used_at).toLocaleDateString() })
                          : t("pats.neverUsed")}
                      </p>
                    </div>
                    <Button
                      variant="outline"
                      size="icon"
                      className="size-8 shrink-0 text-[var(--text-secondary)] hover:text-[var(--danger)]"
                      title={t("pats.revoke")}
                      onClick={() => setRevokeTarget(pat)}
                    >
                      <Icon name="close" size={15} />
                    </Button>
                  </div>
                ))}
              </div>
            )}
          </div>
        </>
      )}

      <ConfirmDialog
        open={revokeTarget !== null}
        title={t("pats.revokeTitle")}
        message={t("pats.revokeConfirm", { name: revokeTarget?.name ?? "" })}
        confirmLabel={t("pats.revoke")}
        cancelLabel={t("pats.cancel")}
        destructive
        icon="close"
        loading={revokePAT.isPending}
        onConfirm={confirmRevoke}
        onCancel={() => setRevokeTarget(null)}
      />
    </div>
  );
}

// RegisteredOAuthClientsSection lists every dynamically self-registered
// OAuth client (RFC 7591 registration is unauthenticated by design) and
// lets a superadmin delete one — the only lifecycle control that exists
// today, but a complete one: deleting a client cascades to every
// authorization code and access/refresh token pair it ever issued
// (oauth_client_store.go's DeleteOAuthClient). Superadmin-only, mirroring
// deploy-provider/GitHub config's instance-wide scope; not shown to a
// regular admin at all rather than shown disabled, matching
// GitHubIntegrationPage's pattern.
function RegisteredOAuthClientsSection() {
  const { t } = useTranslation();
  const { data: me, isLoading: meLoading } = useQuery({
    queryKey: ["me"],
    queryFn: async () => {
      const res = await fetch("/dashboard/api/me", { credentials: "include" });
      if (!res.ok) return null;
      return res.json() as Promise<{ role: string }>;
    },
    retry: false,
  });
  const { data: clients, isLoading } = useOAuthClients();
  const deleteClient = useDeleteOAuthClient();
  const [deleteTarget, setDeleteTarget] = useState<RegisteredOAuthClient | null>(null);

  if (meLoading || me?.role !== "superadmin") return null;

  const confirmDelete = () => {
    if (!deleteTarget) return;
    deleteClient.mutate(deleteTarget.id);
    setDeleteTarget(null);
  };

  return (
    <div className="flex flex-col gap-3">
      <div>
        <h3 className="text-[13px] font-semibold text-[var(--text-primary)]">{t("oauthClients.title")}</h3>
        <p className="mt-1 text-[12.5px] text-[var(--text-secondary)]">{t("oauthClients.explainer")}</p>
      </div>

      {isLoading ? (
        <LoadingState rows={2} />
      ) : !clients?.length ? (
        <EmptyState icon="link" title={t("oauthClients.empty")} description={t("oauthClients.emptyDesc")} />
      ) : (
        <div className="flex flex-col gap-2">
          {clients.map((client) => (
            <div
              key={client.id}
              className="flex items-center justify-between rounded-[10px] border border-[var(--border)] bg-[var(--sunken)] px-4 py-3"
            >
              <div className="flex min-w-0 flex-col gap-0.5">
                <p className="truncate text-sm font-semibold text-[var(--text-primary)]">{client.name}</p>
                <p className="truncate text-[11px] text-[var(--text-tertiary)]">
                  {client.redirect_uris.join(", ")}
                </p>
              </div>
              <Button
                variant="outline"
                size="icon"
                className="size-8 shrink-0 text-[var(--text-secondary)] hover:text-[var(--danger)]"
                title={t("oauthClients.delete")}
                onClick={() => setDeleteTarget(client)}
              >
                <Icon name="close" size={15} />
              </Button>
            </div>
          ))}
        </div>
      )}

      <ConfirmDialog
        open={deleteTarget !== null}
        title={t("oauthClients.deleteTitle")}
        message={t("oauthClients.deleteConfirm", { name: deleteTarget?.name ?? "" })}
        confirmLabel={t("oauthClients.delete")}
        cancelLabel={t("pats.cancel")}
        destructive
        icon="close"
        loading={deleteClient.isPending}
        onConfirm={confirmDelete}
        onCancel={() => setDeleteTarget(null)}
      />
    </div>
  );
}

// MCPPage: dedicated screen replacing the old key-icon-triggered
// PersonalAccessTokens modal (.specs/features/mcp-settings-page). Explains
// the MCP endpoint, both supported auth methods, gives copy-pasteable
// client config (snippets mirrored from README.md's "MCP Server" section),
// and hosts PAT create/list/revoke inline.
export default function MCPPage() {
  const { t } = useTranslation();

  return (
    <div className="flex flex-col gap-8">
      <PageHeader title={t("mcp.title")} subtitle={t("mcp.subtitle")} />
      <EndpointBlock t={t} />
      <AuthExplainer t={t} />
      <ClientTutorials t={t} />
      <PersonalAccessTokensSection />
      <RegisteredOAuthClientsSection />
    </div>
  );
}
