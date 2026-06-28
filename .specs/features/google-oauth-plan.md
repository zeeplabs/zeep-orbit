# Google OAuth — Plano de Implementação

## Sumário

Login no dashboard via "Sign in with Google". O superadmin configura as credenciais e
domínios permitidos via env vars. Usuários com domínio autorizado são auto-cadastrados
na primeira vez que logam.

---

## 1. Configuração (env vars — superadmin)

| Env var | Obrigatório | Descrição |
|---|---|---|
| `GOOGLE_CLIENT_ID` | sim | Client ID do Google Cloud Console |
| `GOOGLE_CLIENT_SECRET` | sim | Client Secret |
| `GOOGLE_REDIRECT_URL` | sim | Ex: `https://orbit.zeeplabs.com/dashboard/api/auth/google/callback` |
| `GOOGLE_ALLOWED_DOMAINS` | não | Domínios permitidos separados por vírgula. Se vazio, qualquer email com conta Google pode logar. Ex: `zeeplabs.com, zeepfy.com` |

**Onde configurar:** Env vars no Pod (K8s Secret), mesma forma que
`DASHBOARD_BOOTSTRAP_SECRET`.

**Backward compatibility:** nenhuma — Google OAuth só aparece se
`GOOGLE_CLIENT_ID` estiver setado.

---

## 2. Fluxo

```
[LoginPage] → clica "Sign in with Google"
    → GET /dashboard/api/auth/google/login
    → server gera state token (CSRF), salva em sessão temporária, redireciona para
      accounts.google.com/o/oauth2/auth

[Google] → autoriza → redireciona para callback URL

[Callback] GET /dashboard/api/auth/google/callback?code=...&state=...
    → valida state token
    → troca code por access_token + id_token (POST para googleapis.com)
    → extrai email, name, sub do id_token
    → verifica allowed_domains:
        - se configurado, o @ do email precisa estar na lista
    → procura usuário por email em dashboard_users:
        a. Se existe → cria sessão (mesmo fluxo do login por email) → redirect /dashboard
        b. Se não existe → cria dashboard_user com role='admin' (auto-registro) → cria sessão → redirect /dashboard
    → redireciona para /dashboard com cookie de sessão
```

### 2.1 State token (CSRF)

- Gerado com `crypto/rand` (32 bytes hex)
- Guardado em cache LRU na memória (mapa com TTL de 10 min)
- Não precisa de tabela no banco — é rápido e auto-limpante
- Removido após uso

### 2.2 TTL do cache de state

```go
type googleState struct {
    token     string
    expiresAt time.Time
}
```

Cache: `sync.Map` com verificação de `time.Now().After(expiresAt)`.

---

## 3. Endpoints

### 3.1 `GET /dashboard/api/auth/google/login`

Redireciona o browser para o Google OAuth consent screen.

**Query params aceitos (opcional):**
- `redirect_to` — para onde voltar após login (default: `/dashboard`)

**Response:** HTTP 302 redirect para URL do Google.

**Erros:**
- 503 se `GOOGLE_CLIENT_ID` não estiver configurado

### 3.2 `GET /dashboard/api/auth/google/callback`

Troca o code do Google por um session cookie.

**Query params:**
- `code` — authorization code do Google
- `state` — state token (CSRF)

**Response:** HTTP 302 redirect para `/dashboard` com cookie `zeep_session` setado.

**Erros:**
- 400 se state inválido/expirado
- 401 se o email não pertence a um domínio permitido
- 500 se a troca do code falhar

---

## 4. Auto-registro

Quando o email do Google não existe em `dashboard_users`:

1. Criar `DashboardUser{Email, Role: "admin", PasswordHash: ""}`
   - `password_hash` vazio indica "conta somente Google" — login por email bloqueado
   - Futuramente pode-se adicionar "vincular senha" nas settings do perfil
2. Se `google_id` for armazenado, adicionar coluna opcional em `dashboard_users`

**Decisão:** `password_hash` vazio NÃO quebra o login por email porque o
bcrypt.Compare devolve erro. O handler de login por email checa isso e retorna
"use Google to sign in" em vez de "invalid credentials".

### 4.1 Coluna google_id (opcional)

```sql
ALTER TABLE zeep_system.dashboard_users ADD COLUMN google_id TEXT UNIQUE;
```

Isso permite:
- Relink da conta Google mesmo se o email mudar
- Prevenir duplicidade de conta Google

**Decisão:** implementar `google_id` desde o início para evitar migração futura.

---

## 5. Modelo de dados

### dashboard_users (alteração)

```go
type DashboardUser struct {
    ID           string    `json:"id"`
    Email        string    `json:"email"`
    PasswordHash string    `json:"-"`
    GoogleID     string    `json:"-"`
    Role         string    `json:"role"`
    CreatedAt    time.Time `json:"created_at"`
}
```

**Provisioner** adiciona:

```sql
ALTER TABLE zeep_system.dashboard_users ADD COLUMN IF NOT EXISTS google_id TEXT;
CREATE INDEX IF NOT EXISTS idx_dashboard_users_google_id
    ON zeep_system.dashboard_users(google_id);
```

### Config (nova struct)

Em `internal/config/types.go` ou em um novo arquivo:

```go
type GoogleOAuthConfig struct {
    ClientID       string   `yaml:"client_id"`
    ClientSecret   string   `yaml:"client_secret"`
    RedirectURL    string   `yaml:"redirect_url"`
    AllowedDomains []string `yaml:"allowed_domains"`
}
```

Carregado de env vars no startup:

```go
func LoadGoogleOAuthConfig() *GoogleOAuthConfig {
    raw := os.Getenv("GOOGLE_ALLOWED_DOMAINS")
    var domains []string
    if raw != "" {
        domains = strings.Split(raw, ",")
        for i := range domains {
            domains[i] = strings.TrimSpace(domains[i])
        }
    }
    return &GoogleOAuthConfig{
        ClientID:       os.Getenv("GOOGLE_CLIENT_ID"),
        ClientSecret:   os.Getenv("GOOGLE_CLIENT_SECRET"),
        RedirectURL:    os.Getenv("GOOGLE_REDIRECT_URL"),
        AllowedDomains: domains,
    }
}
```

(Não precisa ir no apps.yaml — é config do dashboard, não do app.)

---

## 6. Backend — arquivos novos

### `internal/dashboard/google.go`

```go
package dashboard

// GoogleOAuthHandler contém os endpoints e a lógica OAuth.
type GoogleOAuthHandler struct {
    pool         *pgxpool.Pool
    config       *config.GoogleOAuthConfig
    stateCache   *sync.Map   // map[string]googleState
    stateTTL     time.Duration
}

func NewGoogleOAuthHandler(pool *pgxpool.Pool, cfg *config.GoogleOAuthConfig) *GoogleOAuthHandler
func (h *GoogleOAuthHandler) Login(w http.ResponseWriter, r *http.Request)
func (h *GoogleOAuthHandler) Callback(w http.ResponseWriter, r *http.Request)

// internos:
func (h *GoogleOAuthHandler) generateState() string
func (h *GoogleOAuthHandler) validateState(token string) bool
func (h *GoogleOAuthHandler) exchangeCode(code string) (*googleToken, error)
func (h *GoogleOAuthHandler) verifyDomain(email string) bool
func (h *GoogleOAuthHandler) findOrCreateUser(ctx context.Context, email, googleID string) (*DashboardUser, error)
```

### `internal/dashboard/store.go` — adições

```go
func (s *Store) GetUserByGoogleID(ctx context.Context, googleID string) (*DashboardUser, error)
func (s *Store) CreateGoogleUser(ctx context.Context, email, googleID string) (*DashboardUser, error)
func (s *Store) LinkGoogleID(ctx context.Context, userID, googleID string) error
```

---

## 7. Frontend — mudanças

### LoginPage.tsx

- Se `googleOAuthEnabled` (vindo de `GET /dashboard/api/config`), mostrar botão
  "Sign in with Google" abaixo do form de email
- Botão é um link direto para `/dashboard/api/auth/google/login`

### Config endpoint (`GET /dashboard/api/config`)

Adicionar campo ao response:

```json
{
  "theme": "azure",
  "company_name": "Zeep Tecnologia",
  "logo_url": "",
  "google_oauth_enabled": true
}
```

---

## 8. Roteamento

Em `internal/server/server.go`:

```go
if googleCfg := config.LoadGoogleOAuthConfig(); googleCfg.ClientID != "" {
    googleH := dashboard.NewGoogleOAuthHandler(pool, googleCfg)
    r.Route("/dashboard/api/auth/google", func(r chi.Router) {
        r.Get("/login", googleH.Login)
        r.Get("/callback", googleH.Callback)
    })
}
```

Isso garante que as rotas só existem se configuradas.

---

## 9. Tratamento de erros (frontend)

### Casos:

| Situação | Comportamento |
|---|---|
| Google OAuth não configurado | Botão não aparece |
| State inválido/expirado | Tela de erro com link "Try again" |
| Domínio não permitido | Tela: "Seu email não pertence a um domínio autorizado. Contacte o administrador." |
| Email já existe sem google_id | Login normal por senha funciona. (Futuro: "Vincular conta Google" no perfil.) |
| Google retorna erro | Tela de erro genérica com "Try again" |

Todos os erros do callback são páginas HTML simples (não JSON) porque o
callback é um redirect do Google — o browser está navegando, não fazendo fetch.

---

## 10. Passos de implementação

| # | Tarefa | Arquivos |
|---|---|---|
| 1 | Criar `config.GoogleOAuthConfig` + `LoadGoogleOAuthConfig()` | `internal/config/google.go` |
| 2 | Alterar `DashboardUser` + provisioner para `google_id` | `store.go`, `provisioner.go` |
| 3 | Store: `GetUserByGoogleID`, `CreateGoogleUser`, `LinkGoogleID` | `store.go` |
| 4 | Criar `GoogleOAuthHandler` (Login, Callback, state, token exchange, domain check) | `internal/dashboard/google.go` |
| 5 | Adicionar campo `google_oauth_enabled` em `GET /dashboard/api/config` | `handler.go` |
| 6 | Wire rotas no server, condicional por config | `server.go` |
| 7 | LoginPage: botão "Sign in with Google" condicional | `LoginPage.tsx` |
| 8 | LoginPage: tela de erro para callback failures | `LoginPage.tsx` |
| 9 | Store: findByEmail alterado para informar "use Google" se password_hash vazio | `store.go`, `handler.go` |
| 10 | Testes: handler, store, domain validation | `*_test.go` |
| 11 | Helm: adicionar env vars no chart | `charts/zeep-orbit/templates/` |

---

## 11. Dúvidas em aberto

- **Refresh token:** Google pode fornecer refresh token para acesso offline.
  Por enquanto não precisamos — usamos só o id_token para identificar o usuário.
- **Google profile picture:** Podemos armazenar `avatar_url` futuramente.
- **Vincular Google a conta existente:** Postergado para M3. O superadmin pode
  criar a conta com email + senha e depois o usuário vincula o Google.
- **Múltiplos provedores OAuth:** Futuramente podemos adicionar GitHub,
  Microsoft, etc. A arquitetura do `GoogleOAuthHandler` deve ser pensada para
  ser facilmente extensível.
