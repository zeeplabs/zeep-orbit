# Builder project title: validation on the real model

Date 2026-11-23. Stack: local beta2 with the image built from this branch (`zeep-orbit:title`), the maintainer's browser, the builder's configured provider. Each project was created from the chat home and stopped at the first question card or the plan card (nothing was built, no milestone was approved).

## Names generated at creation (PT-01..PT-03)

| Prompt (short) | Language | Title | Backend name |
| --- | --- | --- | --- |
| Service orders for a phone repair shop | pt-BR | Controle de Ordens de Serviço | `ordens-servico-f98679` |
| Bakery stock with low-stock alerts | pt-BR | Controle de Estoque da Padaria | `estoque-padaria-32ce77` |
| Physiotherapy clinic appointment book | pt-BR | Agenda de Consultas Fisioterapia | `agenda-fisioterapia-8b8cc2` |
| Bookshop stock and reservations | en | Bookshop Stock and Reservations | `bookshop-stock-fb7e5c` |

- Four of four used the model's answer (the titles are not the first words of the message), in the language of the prompt, and the backend names are short and valid. The fallback path was not exercised on the real model; it is covered by unit tests.
- Before this change the same kind of prompt produced `quero-uma-lista-de-pedido-8382e0` and `i-want-an-app-for-my-smal-107d7a`.
- Cost: the naming call is not counted in the session. The whole session up to the first question card was 20K to 31K tokens (about $0.004 for the first one, from the usage strip).

## Plan title and rename (PT-04..PT-07)

- The plan came with a title of its own ("Ordens de Serviço da Oficina"), different from the title generated at creation, and the plan card showed it with the rename button.
- Renaming it in the plan card ("Oficina do Celular") updated the page header at once, set `title_locked = true`, wrote one `builder.session.rename` audit row with the previous title, and left the backend name `ordens-servico-f98679` untouched.
- The header pencil (inline rename) is visible in every project.

## Not verified on the real model

- Approving a plan with an unlocked title (the plan title replacing the generated one): covered by `TestBuilderPlanning_ApproveNamesTheProjectUnlessTheUserRenamedIt`, but no plan was approved on the stack, because that starts a build and spends model credit.
- The naming call failing or timing out against a real provider.
- The sidebar and the backend dropdown with the new names (only the project header was looked at).
- Existing projects keep their old titles and backend names.
