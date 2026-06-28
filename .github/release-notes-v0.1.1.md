## 🚀 Zeep Orbit v0.1.1

Bug fixes, dashboard improvements, auth providers, and open-source polish.

### ✨ Features

#### 🔐 Auth Providers
- **Google OAuth no Dashboard** — configure via UI (Appearance → Google OAuth), credenciais criptografadas AES-256-GCM
- **Provedores de Login por App** — cada app pode ter email e/ou Google OAuth ativado via dashboard, com Client ID/Secret e domínios permitidos
- **`GET /{app}/auth/providers`** — endpoint que lista os provedores ativos para o frontend do app
- **`/{app}/auth/google/login` + callback** — Google OAuth por app, retorna JWT igual ao login por email

#### 🖥️ Dashboard
- **Troca de senha** — qualquer usuário pode alterar própria senha; superadmin pode alterar senha de qualquer usuário
- **Gestão de Usuários por App** — listar, buscar, desativar/reativar contas, resetar sessões, contagem por provider
- **Menu lateral** renomeado de "Aparência" para "Configurações"
- **Cache limpo ao login/logout** — dados de usuário anterior não aparecem mais após troca de conta

#### 📦 Open Source
- **Documentação Docusaurus** (`/docs`) com deploy automático via GitHub Pages
- **Diagrama de arquitetura SVG**
- **Playwright E2E tests** para dashboard
- **Badges** no README (CI, License, Go, Docker, Release)
- `SECURITY.md`, `CODE_OF_CONDUCT.md`, `SUPPORT.md`
- **GitHub Discussions** ativado
- Go reference docs (package comments)
- `CHANGELOG.md` e `ROADMAP.md` atualizados

### 🐛 Bug Fixes
- **Login 500** — `google_id` NULL causava panic (COALESCE fix)
- **FK violation** no Data Browser create (owner_id removido)
- **Cache React Query** não era limpo ao trocar de usuário
- **App Users 500** — colunas `active`/`provider` ausentes em apps existentes

### 🔒 Security
- **AES-256-GCM** para secrets em repouso (OAuth client credentials)
- Encrypted storage no banco com revelação condicional

### 📦 Docker
```bash
docker pull ghcr.io/zeeplabs/zeep-orbit:v0.1.1
```

### 📋 Helm
```bash
helm repo add zeeplabs https://zeeplabs.github.io/zeep-orbit
helm install zeep-orbit zeeplabs/zeep-orbit
```

---

Built with ❤️ by [Zeep Tecnologia](https://zeeptech.com.br)
