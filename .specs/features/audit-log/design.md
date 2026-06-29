# Audit Log — Design

## Context

M3 Governance feature. Rastreia todas as ações modificadoras feitas no dashboard por admins/superadmins. Visível apenas para superadmin.

## Tabela

`zeep_system.audit_log`:

| Coluna | Tipo | Descrição |
|---|---|---|
| id | UUID PK | gen_random_uuid() |
| user_id | UUID FK → dashboard_users | Quem fez |
| user_email | TEXT | Email no momento da ação (desnormalizado) |
| action | TEXT | `app.create`, `user.delete`, etc |
| resource_type | TEXT | `app`, `user`, `config`, `session`, `auth_provider`, `data` |
| resource_id | TEXT | UUID do recurso afetado |
| resource_name | TEXT | Nome legível (ex: nome do app, email do usuário) |
| metadata | JSONB | Payload específico da ação |
| ip_address | TEXT | RemoteAddr |
| created_at | TIMESTAMPTZ | now() |

Índices: `created_at DESC`, `action`, `user_id`.

## Store

`internal/dashboard/audit_store.go` — duas funções:
- `InsertAuditLog(ctx, pool, userID, userEmail, action, resourceType, resourceID, resourceName, metadata, ip)` — INSERT
- `ListAuditLog(ctx, pool, filter)` — SELECT paginado com filtros action/user

## Handler

- `GET /dashboard/api/audit-log?limit=&offset=&action=&user=` → `ListAuditLog` (superadmin only)
- Helper `h.audit(ctx, ...)` chamado no final de cada handler modificador

## Ações capturadas

| Handler | action | resource_type | resource_name |
|---|---|---|---|
| CreateApp | `app.create` | app | app.Name |
| UpdateApp | `app.update` | app | app.Name |
| DeleteApp | `app.delete` | app | app.Name |
| Bootstrap | `bootstrap.complete` | user | email |
| Login | `user.login` | session | email |
| Logout | `user.logout` | session | email |
| CreateUser | `user.create` | user | email |
| DeleteUser | `user.delete` | user | email |
| ChangeMyPassword | `user.password.change` | user | email |
| ChangeUserPassword | `user.password.change` | user | target email |
| UpdateConfig | `config.update` | config | — |
| UpsertAuthProvider | `auth.provider.update` | auth_provider | provider name |
| DeactivateAppUser | `app.user.deactivate` | app_user | app + user email |
| ActivateAppUser | `app.user.activate` | app_user | app + user email |
| ResetAppUserSessions | `app.user.sessions.reset` | app_user | app + user email |
| DataBrowserCreate | `data.create` | data | app/table |
| DataBrowserUpdate | `data.update` | data | app/table |
| DataBrowserDelete | `data.delete` | data | app/table |

## Frontend

- `AuditLogPage.tsx` — tabela com colunas: timestamp, ação (badge), usuário, recurso, metadata, IP
- Filtros: select de ação, busca por email
- Paginação (50 por página)
- Sidebar: item "Auditoria" (superadmin only, entre Usuários e Configurações)
- Rota: `/auditoria`
