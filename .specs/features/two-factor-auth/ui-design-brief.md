# Two-Factor Authentication (2FA) — Brief pra Design de Telas

Contexto: zeep-orbit é um backend-as-a-service self-hosted (dashboard React já existente). Esta feature adiciona 2FA (TOTP + backup codes) em dois lugares simétricos: **perfil do próprio dashboard** (qualquer admin/superadmin pode ativar; superadmin pode exigir de todos) e **config do app hospedado** (criador do app permite/exige pros usuários finais do app). Ref técnica completa: `.specs/features/two-factor-auth/{spec,design}.md`.

Design system existente do dashboard deve ser seguido (não inventar estilo novo).

---

## Telas/componentes a desenhar

### 1. Página "Segurança" (perfil do usuário do dashboard, nova)

**Propósito**: usuário ativa/desativa a própria 2FA.

**Estados**:

| Estado | O que mostra |
|---|---|
| **2FA desabilitada** | Card explicando o que é 2FA, botão "Ativar 2FA" |
| **2FA habilitada** | Badge verde "Ativa", botão "Desativar" (some/desabilita se `require_2fa` estiver ligado pra essa conta — mostrar por quê, não só esconder), botão "Regenerar códigos de backup" |
| **`require_2fa` ativo pra essa conta, sem 2FA configurada ainda** | Estado de destaque (não é erro, é obrigação pendente): "Sua conta exige 2FA — configure agora para continuar acessando" |

**Conta Google OAuth**: página inteira não se aplica — mostrar mensagem explicando que a segurança da conta é gerenciada pelo Google, sem oferecer toggle nenhum (nunca mostrar toggle "desabilitado" que na verdade é "não aplicável").

---

### 2. Modal de setup (fluxo de 3 passos, usado tanto no dashboard quanto no app)

1. **QR code**: renderiza `otpauth://` como QR code (lib nova, ver design.md), com opção de copiar o secret manualmente (pra quem não consegue escanear).
2. **Confirmação**: campo de 6 dígitos, botão "Confirmar". Erro inline se código errado, sem fechar o modal.
3. **Backup codes**: exibidos **uma única vez**, formato lista de 8 códigos (`XXXX-XXXX`), botão "Copiar todos" e/ou "Baixar .txt", aviso bem visível (não um tooltip discreto) tipo "Guarde estes códigos agora — não mostraremos de novo". Botão final "Concluir" só habilita depois de uma confirmação explícita tipo checkbox "já salvei meus códigos".

---

### 3. Tela de login com 2FA (step-up)

- Login normal (email+senha) continua igual.
- Se a conta tem 2FA: em vez de redirecionar direto, mostra uma segunda tela/passo: campo de código (6 dígitos ou backup code, mesmo campo aceita os dois formatos), botão "Verificar".
- Erro de código errado: mensagem inline, sem reiniciar o login inteiro (usuário não digita a senha de novo).
- Sem contagem regressiva visível do TTL do token de step-up (5 min) — se expirar, erro claro pede pra fazer login de novo, sem UI de "tempo restante" (desnecessário, YAGNI).

---

### 4. Config do app (criador do app) — seção "2FA para usuários"

- Dois toggles na página de config do app já existente: "Permitir 2FA" e "Exigir 2FA" (o segundo só habilita se o primeiro estiver ligado — desabilitado visualmente com tooltip explicando a dependência).
- Nenhuma tela nova de "gerenciar 2FA dos usuários finais" além disso — é config do app, não um painel de usuários (isso já existe em outro lugar do produto).

---

### 5. Config de plataforma (superadmin) — "Exigir 2FA de todos os admins"

- Um toggle simples numa página de config de plataforma já existente (ou nova, se não houver uma — confirmar com o dashboard atual). Sem confirmação de dupla etapa necessária (é reversível, não é destrutivo).

---

## Interações específicas

- **Tentativa de desativar 2FA com `require_2fa` ativo**: botão "Desativar" mostra erro claro explicando que a conta/app exige 2FA — nunca falha silenciosamente.
- **Reset administrativo** (superadmin resetando 2FA de outro admin, ou criador do app resetando de um usuário final): ação em uma tela de gestão de usuários já existente (não uma tela nova) — botão "Resetar 2FA" com confirmação simples (é uma ação sensível mas reversível pelo próprio usuário depois, re-configurando).

---

## Coisas que o design PRECISA respeitar

- **i18n obrigatório**: toda string por `react-i18next`, en+pt-BR desde o dia 1, inclusive o aviso de backup codes
- **Erro de mutação sempre visível**: toast (`sonner`), nunca silencioso
- **Backup codes exibidos uma única vez**: nunca desenhar uma tela que permita "ver os códigos de novo" depois de fechar o modal de setup
- **Conta Google OAuth**: nunca mostrar toggle de 2FA como se fosse aplicável — mensagem explicativa clara em vez disso
- **`must_setup_2fa` não é erro**: tom de "próximo passo obrigatório", não de bloqueio/penalidade

---

## Fora de escopo deste brief

- Painel de gestão de usuários em si (onde o botão "Resetar 2FA" vai morar) — já existe no produto, esta feature só adiciona uma ação nele
- WebAuthn/chaves físicas — não faz parte desta feature (ver Out of Scope do spec)
- SMS/email como método alternativo — não existe nesta feature
