# Enterprise Licensing Design

**Spec**: `.specs/features/enterprise-licensing/spec.md`
**Status**: Draft — contrato com `license-server` verificado contra a implementação real em 2026-08-21 (repo `zeep-license-server` já existe, ~70-80% pronto pro escopo v1 dele); nenhuma linha de `internal/enterprise/license` foi escrita ainda no zeep-orbit

---

## Architecture Overview

Pacote novo `internal/enterprise/license` faz toda a verificação/gating do lado do zeep-orbit. Nenhuma dependência de rede é obrigatória — o `license-server` (repo separado da Zeep Labs) é opcional e só entra em jogo se `LICENSE_SERVER_URL` estiver configurado, para revogação. A pasta `internal/enterprise/` inteira (não só `license/`) vive sob `internal/enterprise/LICENSE` própria; o `LICENSE` raiz do repositório passa a ser um disclosure multi-licença apontando pra ela, igual ao padrão GrowthBook.

```mermaid
graph TD
    ENV["LICENSE_KEY / LICENSE_SERVER_URL\n(env vars)"] --> BOOT[cmd/zeep boot]
    BOOT -->|"license.Load(key)"| VERIFY[internal/enterprise/license.Verify]
    VERIFY -->|"Ed25519 pub key embutida"| RESULT["*License\n(plan, ref, exp, trial)"]
    RESULT --> STATE["licenseState\n(in-memory, RWMutex)"]
    REFRESH["goroutine periódica\n(se LICENSE_SERVER_URL setado)"] -->|"GET /v1/status?product=orbit&ref=..."| LS[("license-server\n(repo separado, já existe:\nzeep-license-server)")]
    LS -->|"ref, revoked, expires_at"| REFRESH
    REFRESH -->|"atualiza se revogado"| STATE
    STATE -->|"HasFeature(license, feature)"| HANDLERS["Handlers gated\n(inline check, mesmo padrão de ResolveAppRole)"]
    STATE -->|"GET /dashboard/api/license/status"| UI["Dashboard UI\nuseFeature(name)"]
    LS -.->|"Stripe webhook\ncheckout.session.completed"| ISSUE["gera + assina key\n(chave privada só aqui)"]
```

---

## Code Reuse Analysis

### Existing Components to Leverage

| Component | Location | How to Use |
|---|---|---|
| Padrão de checagem inline (`role == "superadmin"`, `ResolveAppRole`) | `internal/dashboard/*.go` | `HasFeature` chamado do mesmo jeito, sem middleware central novo |
| `usePublicConfig()` | `internal/dashboard/ui/src/lib/api.ts` | `GET /dashboard/api/license/status` entra nesse mesmo hook compartilhado, evita fetch ad-hoc |
| Config via env var + tabela de configuração no README | `README.md` (seção de configuração) | `LICENSE_KEY`, `LICENSE_SERVER_URL`, `LICENSE_REFRESH_INTERVAL` documentadas ali, seguindo `AGENTS.md` |
| Audit log | `internal/dashboard/audit_store.go` | Evento `license.loaded` (plan resolvido) e `license.revoked` no momento da transição |
| Padrão de goroutine periódica com lock | Nenhum idêntico encontrado no repo — introduzido aqui como novo padrão, mas segue o mesmo estilo de simplicidade (sem lib de scheduling externa, `time.Ticker` simples) | Reuso interno apenas |

### Integration Points

| System | Integration Method |
|---|---|
| `cmd/zeep` (boot) | Chama `license.Load()` uma vez na inicialização, guarda resultado em estado global do processo (mesmo nível de "config global" que outras flags de boot) |
| `license-server` (externo, opcional — **já existe hoje**: repo `zeep-license-server`, workspace ZeepLabs) | `GET {LICENSE_SERVER_URL}/v1/status?product={slug}&ref={ref}` — único endpoint consumido pelo zeep-orbit; `product` é obrigatório (servidor atende vários produtos, não só o orbit); contrato de resposta fixado abaixo, verificado contra a implementação real em 2026-08-21 |
| Dashboard UI | Novo endpoint `GET /dashboard/api/license/status`, novo hook `useFeature`, nova página "Licença" |

---

## Components

### `internal/enterprise/license.License` (tipo + verificação)

- **Purpose**: Decodificar, verificar assinatura e expor o plano/metadados de uma license key.
- **Location**: `internal/enterprise/license/license.go`
- **Interfaces**:
  ```go
  type Plan string

  const (
      PlanOSS        Plan = "oss"
      PlanEnterprise Plan = "enterprise"
  )

  type License struct {
      Org          string // ID interno da organização no license-server (UUID) — não é nome/slug legível
      Product      string // slug do produto no license-server; SHALL ser "orbit" (constante) — payload de outro produto é tratado como inválido
      Plan         Plan
      Ref          string
      Trial        bool
      IssuedAt     time.Time
      ExpiresAt    time.Time  // corte duro; já inclui a graça de D-134 — nunca nil na prática, o license-server sempre emite com duração finita
      RenewalDueAt time.Time  // data real de cobrança/vencimento, só para exibição de aviso (nunca usado em lógica de gating)
  }

  // Verify decodifica e valida a assinatura Ed25519 de uma key no formato
  // base64url(payload).base64url(signature). Nunca retorna erro fatal ao
  // chamador de boot — erros de verificação, incluindo Product != "orbit",
  // resultam em License{Plan: PlanOSS}.
  func Verify(key string) *License

  func (l *License) IsExpired() bool
  ```
- **Dependencies**: `crypto/ed25519` (stdlib), chave pública embutida em `internal/enterprise/license/publickey.go` (const, análoga a `public-key.ts` do GrowthBook) — chave pública é por produto no license-server (`GET /admin/v1/products/{slug}`), a embutida aqui é a do produto `orbit` especificamente
- **Reuses**: nenhum componente pré-existente (pacote novo, sem precedente no repo)

> **Contrato real verificado contra `zeep-license-server` em 2026-08-21** (repo existe, ~70-80% pronto pro escopo v1 dele — ver D-211 no vault): a struct acima já reflete o payload real emitido por aquele servidor, não mais a suposição original deste spec (que não tinha `Product` e assumia `Org` como nome legível). Ver `Data Models` abaixo pro payload exato.

### `internal/enterprise/license.Registry` (gating)

- **Purpose**: Mapear plano → features habilitadas, expor `HasFeature`.
- **Location**: `internal/enterprise/license/features.go`
- **Interfaces**:
  ```go
  type Feature string

  // Cada feature enterprise futura adiciona uma constante aqui.
  // (Nenhuma feature concreta é definida por este spec — ver Out of Scope.)

  var planFeatures = map[Plan][]Feature{
      PlanEnterprise: { /* preenchido conforme features forem criadas */ },
  }

  func HasFeature(l *License, f Feature) bool
  ```
- **Dependencies**: `License`
- **Reuses**: nenhum

### `internal/enterprise/license.State` (estado do processo + refresh)

- **Purpose**: Guardar a licença ativa em memória, rodar refresh periódico de revogação.
- **Location**: `internal/enterprise/license/state.go`
- **Interfaces**:
  ```go
  func Load(key string) *State           // chamado uma vez no boot
  func (s *State) Current() *License     // leitura thread-safe (RLock)
  func (s *State) StartRefresh(ctx context.Context, serverURL string, productSlug string, interval time.Duration)
  ```
  `productSlug` SHALL ser a constante `"orbit"` (montada no `GET /v1/status?product={productSlug}&ref={ref}` real do `zeep-license-server` — não existia como parâmetro na versão original deste design)
- **Dependencies**: `Verify`, cliente HTTP simples (`net/http`, timeout curto, sem retry — se falhar, mantém estado atual)
- **Reuses**: nenhum padrão de scheduling existente no repo (novo)

### `internal/dashboard.LicenseHandler`

- **Purpose**: Expor status da licença pro frontend.
- **Location**: `internal/dashboard/license.go`
- **Interfaces**:
  - `GET /dashboard/api/license/status` — público (mesmo nível de `usePublicConfig`), retorna `{plan, features: []string, org, trial, expires_at}` (nunca vaza a key crua nem o `ref` completo — só o necessário pra UI)
- **Dependencies**: `internal/enterprise/license.State`
- **Reuses**: roteamento padrão do dashboard (`internal/dashboard/router.go` ou equivalente já existente)

### `internal/dashboard/ui/src/enterprise/useFeature.ts`

- **Purpose**: Hook de consulta de feature habilitada no frontend.
- **Location**: `internal/dashboard/ui/src/enterprise/useFeature.ts`
- **Interfaces**: `function useFeature(name: string): boolean`
- **Dependencies**: `usePublicConfig()` (estende o config público já carregado, não faz fetch próprio)
- **Reuses**: `usePublicConfig()` — licença entra como mais um campo do mesmo payload carregado antes do primeiro paint (regra do `AGENTS.md` de frontend)

---

## Data Models

Nenhuma tabela nova em `zeep_system` é necessária no lado do zeep-orbit — o estado de licença é resolvido em memória a partir da env var no boot, não persistido no banco da aplicação. Isso evita acoplar o schema-per-app do produto a um conceito que é global ao processo, não por-app.

**Payload da license key** — versão real, verificada contra `internal/license/token.go` do `zeep-license-server` em 2026-08-21 (substitui o exemplo hipotético anterior deste design, que não tinha `product`/`renewal_due_at` e assumia timestamps como string ISO):

```json
{
  "org": "3fa2b9e0-...-uuid",
  "product": "orbit",
  "plan": "enterprise",
  "ref": "3c9d1a44-...-uuid",
  "iat": 1785600000,
  "exp": 1817222400,
  "renewal_due_at": 1817136000,
  "trial": false
}
```

Diferenças que importam pra `Verify`/T-01:
- `iat`/`exp`/`renewal_due_at` são **Unix timestamp inteiro** (`int64`, segundos), não string ISO 8601 — `Verify` decodifica direto pra `int64` e converte com `time.Unix(v, 0)`, sem parse de string.
- `org` e `ref` são UUIDs internos do `zeep-license-server`, não nomes/slugs legíveis — não usar pra exibição direta na UI sem contexto adicional (a UI usa o payload público de `/dashboard/api/license/status`, que pode reformatar).
- `product` SHALL ser validado como `"orbit"`; qualquer outro valor (ou campo ausente) é tratado como licença inválida — mesmo caminho de erro de assinatura inválida (`License{Plan: PlanOSS}`).
- `exp` já vem do servidor com a graça de 7 dias de D-134 embutida (`renewal_due_at + 7 dias` — corrigido no `zeep-license-server` em 2026-08-21, ver D-211; antes disso o servidor calculava ao contrário e não havia graça real nenhuma). `renewal_due_at` é a data real de cobrança, só pra exibição — a suposição original deste design sobre onde a graça vive **estava correta em intenção, e agora também está correta na implementação real do servidor**.

**Contrato de resposta do `license-server`** (`GET /v1/status?product={slug}&ref={ref}`, consumido só pelo refresh de revogação — `product` é obrigatório, ausente na versão original deste design):

```json
{ "ref": "3c9d1a44-...-uuid", "revoked": false, "expires_at": "2027-08-21T00:00:00Z" }
```

`expires_at` é um campo real a mais que o design original não previa (RFC3339 string, não Unix — inconsistente com o formato do payload da key, mas é assim que o servidor responde). Comentário no código do servidor (`internal/api/public_handlers.go`) diz que esse campo existe "pra clientes reforçarem expiração no lado servidor em vez de confiar só no `exp` local assinado, que um cliente adulterado poderia ignorar" — o zeep-orbit pode usar isso como sinal adicional, mas não é obrigatório pro MVP (LIC-20 a LIC-24 seguem cobertos só com `revoked`).

Erro/timeout nessa chamada é tratado como "sem informação nova" — nunca como "revogado" (fail-open pra revogação, fail-closed nunca acontece por indisponibilidade de rede, conforme AC LIC-23 do spec). Isso é responsabilidade do **cliente** (zeep-orbit) — o servidor real não implementa fail-open nem nenhuma lógica de graça no próprio endpoint de status, ele só responde o estado atual do registro.

---

## Error Handling Strategy

| Error Scenario | Handling | User Impact |
|---|---|---|
| Assinatura Ed25519 inválida | `Verify` retorna `License{Plan: PlanOSS}`, loga warning | Nenhum crash; app roda no plano `oss` |
| Payload JSON corrompido | Mesmo tratamento acima | Idem |
| Key expirada (`exp` no passado) | Mesmo tratamento acima | Idem |
| Chave pública desconhecida (rotação) | Erro específico logado (`"unknown public key version"`), mas resultado ainda é `PlanOSS` pro chamador — não derruba boot | Log ajuda suporte a identificar key de versão antiga |
| `product` no payload ≠ `"orbit"` (ou ausente) | Mesmo tratamento de assinatura inválida — `License{Plan: PlanOSS}`, warning logado | Protege contra key emitida pra outro produto do `zeep-license-server` (ele atende vários) ser aceita por engano |
| `LICENSE_SERVER_URL` inacessível no refresh | Mantém `*License` atual sem alteração, loga warning (não error) | Zero impacto no usuário |
| `license-server` confirma revogação | `State` transiciona pra `PlanOSS` no próximo `Current()`, evento `license.revoked` no audit log | Features enterprise somem da UI/API a partir do próximo ciclo, sem restart |
| Endpoint gated chamado sem feature habilitada | 403, corpo em inglês | Toast de erro no frontend (`onError` padrão) |

---

## Tech Decisions (only non-obvious ones)

| Decision | Choice | Rationale |
|---|---|---|
| Algoritmo de assinatura | Ed25519 em vez de RSA-PSS (GrowthBook usa RSA-PSS) | Stdlib Go, sem parâmetro de padding pra errar, assinatura menor — decidido explicitamente no brainstorm, sem motivo pra replicar RSA aqui |
| Onde vive o estado de licença | Memória do processo (global, resolvido no boot), não banco de dados | Licença é config de processo, não dado de aplicação; evita acoplar ao schema-per-app e evita ter que invalidar cache de banco quando a key muda (só precisa reiniciar, que já é o fluxo natural de trocar env var) |
| Cap de seats/usuários | Nenhum — só gating de feature | Decidido explicitamente no brainstorm: licença vitalícia sem conceito de seat seria uma complicação sem valor de negócio real pro modelo escolhido |
| Falha de rede no refresh de revogação | Fail-open (mantém último estado válido) | Indisponibilidade do `license-server` da Zeep Labs nunca deve penalizar o cliente que já tem licença válida — mesmo padrão do GrowthBook |
| Lista de features enterprise concretas | Não definida neste spec/design | Mecanismo é genérico por design; classificar cada feature futura como core/enterprise é decisão de produto tomada quando aquela feature for especificada, não travada aqui (ver Out of Scope do spec) |
| Onde a chave privada de assinatura vive | Só no `license-server`, nunca em nenhum artefato deste repositório | Vazamento da chave privada no zeep-orbit permitiria qualquer pessoa forjar licença enterprise — risco alto, tratado como invariante de segurança, não como detalhe de implementação |
| Contrato `license-server` fixado aqui, implementação lá | Este spec define só o payload da key e o endpoint `/v1/status` | `license-server` é produto separado da Zeep Labs (repo próprio, `zeep-license-server`), com seu próprio ciclo de spec/design/tasks — misturar os dois infla o escopo deste spec e acopla releases desnecessariamente |
| `product` como campo obrigatório no payload e no query param de `/v1/status` | Validado no `Verify` e sempre enviado como `"orbit"` no refresh | Divergência descoberta 2026-08-21 ao auditar a implementação real do `zeep-license-server` (um servidor único atende múltiplos produtos da Zeep Labs) — este design assumia originalmente um contrato 1:1, sem esse campo |

---

## Tips aplicadas

- Reuso verificado por busca no código antes de declarar "não existe" (padrão de goroutine periódica, hook de config público).
- Nenhuma tabela nova em `zeep_system` — decisão explícita registrada em Data Models, não omissão.
- Texto legal da licença enterprise (`internal/enterprise/LICENSE`) permanece fora deste documento — é artefato jurídico, não técnico (ver spec.md).
