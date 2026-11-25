# Builder projects without a backend: validation (T9)

Run 2026-11-25 on the local `orbit-beta2` stack with images built from `develop` at `b395df0` (T2 to T8 merged), Chrome. Build model `claude-sonnet-5-5`, planning model `claude-haiku-5-5`, planning mode required. Every project was created with `backend: {mode: "none"}`, which is what the chat home now sends by default, and the first message was posted the way the UI does. Only the planning turns ran (no approval, no build). Planning questions were answered with "Decide for me". Total cost **US$ 0.0237** for 9 sessions (each US$ 0.0013 to 0.0037). The sessions were deleted afterwards.

Briefs: the five of builder-design-quality (`c1` bike shop customers, `c2` gym schedule, `c3` repair orders, `c4` bakery stock, `e1` bookshop), plus `e2` (dental waiting list, English), `v1` (vague bakery) from `planeval/briefs.json`, and two site briefs written for this run (`s1` pilates studio landing page in pt-BR, `s2` coffee shop one-pager in English).

## Results

| Brief | Kind | Data question asked | Plan |
|---|---|---|---|
| c1 bike shop customers | app, clear | yes (1 question) | static page only; customer list, search and login out of scope; says the data part needs a project with a backend |
| c2 gym schedule | app, clear | yes (scope question + look) | static presentation page; schedule, bookings, login out of scope |
| c3 repair orders | app, clear | yes (1 question) | static page; order list, status filter, mechanic login out of scope |
| c4 bakery stock | app, clear | **no** | a "visual prototype": list, in/out and history screens with empty states, no saving, no login; says real stock control needs a new project with a backend |
| e2 dental waiting list | app, clear | yes (1 question) | static page; the waiting list itself out of scope |
| e1 bookshop | vague | yes (storage, purpose, look) | one-page site, "Static site: no accounts and no stored data" |
| v1 bakery | vague | yes (storage, content, look) | one-page site |
| s1 pilates landing page | site, clear | no | one-page site, WhatsApp link instead of a form |
| s2 coffee shop one-pager | site, clear | no | one-page site, "Static site: no accounts and no stored data" |

Other things the plans did right: no invented business facts (every plan lists what it needs from the user and uses neutral copy until then), no form that goes nowhere (contact by link), no accounts or tables planned in a project with no backend.

## What it means

- **The safety net triggers, and the default still fails apps.** All 5 explicit app briefs ended in a plan that does not build the app. 4 of the 5 asked the data question first; c4 did not. Because "Decide for me" was used, the planner took its static default, so this run does not show what a user who answers "Yes, it has to save information" gets: by design (T7) the plan is the same static part plus the instruction to start a project with a backend. Either way the user has to start again. All 44 earlier builds on this installation were apps with data, so the new default adds this step for the most common request. [Certo] for these 5 briefs; [Provável] that it holds for real users, who may write shorter and vaguer prompts.
- **Sites work.** Both site briefs went straight to a sensible plan with no unneeded question, and the vague briefs asked the right question.
- **c4 is the worst case.** The planner did not ask and planned a prototype with screens that look like an app but save nothing. The plan states it in its first assumption, and the user approves the plan before anything is built, so the cost is a wasted build, not a silent failure. The rule "when the brief describes an app that stores information, ask one question" was followed 4 times out of 5.
- **Language.** For the English briefs `e1` and `e2` the plan assumed Brazilian Portuguese for the app's interface ("the usual language for a shop of this kind", "the clinic name suggests it"), while `s2` kept English. The session locale sent was `en`. [Incerto] whether this predates the series (the planning prompt picks the language from the brief and the interface locale); worth a look on its own.

## Options for the default (the maintainer's decision)

1. **Keep "No backend" as the default** and make "Yes" on the planner question useful: attach a new backend to the session (NB-08, now the most valuable part of phase 2). Until then, the 5 of 5 above are a dead end.
2. **Flip the default back to "New backend"** until NB-08 exists, keeping "No backend" one click away. One-line change in `HomeChatPage.tsx`; the planner question and the `site` template stay. Sites lose one click, apps keep working as before.
3. **Choose from the first message**: if the message reads as an app, the home preselects "New backend"; otherwise "No backend". Needs a way to tell them apart before the session exists (a keyword rule is brittle; the planner already does it, one step too late).

Recommendation: 2 now, then 1 when NB-08 lands. Reason: the measurement shows the planner already recognizes apps, but the project it recognizes them in cannot become one.

## Not covered

- One run per brief, one model pair, planning only: no build was run on the `site` template with the model, so how it follows the "Projects with no backend" section while writing code is still unmeasured (a later run with one approved plan would show it).
- The answer "Yes" was not exercised.
- `orbit-react-patterns` visibility in no-backend sessions was not observable in a planning-only run.
- The advanced new-session dialog does not offer "No backend" and was not part of this run.

## Found while preparing the run (separate fix)

`useStartBuilderProject` keeps a pending session keyed by `backendKey`, and `none` and `new` both map to `'new'`. If the first send fails and the user switches between "No backend" and "New backend", the retry would reuse the session created for the other choice. Fix: key it by the mode. Not fixed in this branch.
