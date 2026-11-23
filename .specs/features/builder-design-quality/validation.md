# AI Builder Design Quality Validation

**Spec**: `spec.md` (DQ-20). Evidence file: rubric, run protocol and results. Numbers are written here only after a run; nothing below is a measured result until a row is filled.

## 1. Rubric (score each 1-5, blind)

A reviewer scores a screenshot (desktop width, and phone width if available) without knowing which build produced it.

| Criterion | 1 | 3 | 5 |
| --- | --- | --- | --- |
| Hierarchy | Everything has the same weight; nothing leads the eye | A clear heading and primary action, the rest flat | One clear focal point, ranked groups, whitespace carries structure |
| Distinctiveness | Indistinguishable from a default template; same look as other briefs | Some project-specific choices (color, copy) on a common layout | Palette, type and density clearly fit this project and differ from the other briefs |
| State coverage | Only the happy path visible; empty/loading/error missing | Empty and error handled, plain | Loading, empty, error and success all designed |
| Contrast and legibility | Text under 4.5:1 in places or tiny | Readable, minor misses | Passes contrast everywhere; sizes comfortable |
| Motion with purpose | None, or on everything | Some, generic | Few, tied to actions and state changes; reduced-motion respected |
| Kit usage (P2 only) | Re-implements primitives | Mixed | Uses the kit's components and tokens throughout |

Cross-brief checks (scored once per run, not per brief):

- Do two different project kinds share the same palette and type pairing? (yes/no, list which)
- Does any brief show the known tells (gradient wash, identical rounded cards with the same soft shadow, an arrow on every button, one word of a headline in another color)? (list)

## 2. Run protocol (manual, until T14)

Preconditions the maintainer confirms before the first run: provider and model, per-session cost cap, and that spending credits for this is accepted.

1. Stack: the `orbit-beta2` local stack (images 1.12.0-beta.2) for the baseline; the stack built from this branch for later runs. Record the image tag.
2. One fresh builder session per brief, default template, same model and same interface language for every run. Use the brief text from `internal/agent/testdata/planeval/briefs.json` verbatim, answer planning questions with the planner's own suggested options ("decide for me" where offered), approve the plan, let the build finish. Do **not** add any follow-up asking for a better look; that is the point.
3. Briefs (5): `c1` bike shop customers, `c2` gym class schedule, `c3` repair orders, `c4` bakery stock, `e1` bookshop in English.
4. Capture per brief: screenshot of the main screen after the first build (preview), the rendered brief (plan) text and its character count, model, tokens and cost shown by the timeline, which skills were loaded (timeline), number of turns.
5. Score with the rubric. At least one reviewer who did not run the session, with the builds shuffled and unlabeled. If only one reviewer exists, say so in the results.
6. Store screenshots outside the repo (size); record their folder here.

## 3. Baseline results (T1)

Run 2026-11-23 on the `orbit-beta2` stack (images 1.12.0-beta.2), Chrome, dashboard UI language English, default template, no company persona or rules, builtin skills v1 (`orbit-frontend-design` v1). Models: build `claude-sonnet-5-5`, planning `claude-haiku-5-5`, planning mode Required. Briefs verbatim from `planeval/briefs.json`; every planning question answered with "Decide for me"; plan approved as proposed; backend proposals confirmed; "Continue" used for each milestone; no follow-up about visuals. Run by Claude, not blind: the scoring columns of the rubric are left empty for a blind reviewer (section 1). Screenshots are outside the repo (the maintainer's scratchpad, to be moved to a stable folder).

### Facts recorded

| Brief | Plan language | Planner questions | Plan `Look` (style / colors) | Milestones built | Cost | Tokens |
| --- | --- | --- | --- | --- | --- | --- |
| c1 bike shop | English (brief in pt-BR) | none | "Simple, clean, plain forms and a single list" / "Blue and white" (the brief asked for it) | 3/3 | $0.4595 | 518K at M3 start; final not read |
| c2 gym | pt-BR | 4 (language, bookings, front desk, look) | "Clean, light look with one accent color, phone-friendly" / none | 3/3 | $0.5746 | 582K |
| c3 repair | pt-BR | 1 | "Tema escuro, limpo, com cartões e status em cores" / "Fundo escuro com destaque em cor de acento; cada status com uma cor própria" (brief asked for dark) | 3/3 | $0.5405 | 545K |
| c4 bakery | English (brief in pt-BR) | none | "Clean and simple, with a clear warning colour for products below the minimum" / "Warm bakery tones on a light background; red for alerts" | 4/4, M1 failed once (see below) | $0.8324 | 1.1M |
| e1 bookshop | English | 4, all decided by the planner | "Simple, clean and easy to read on a phone at the till: light background, large text, clear buttons" / none | 3/3 | $0.4943 | 502K |

Total: $2.90 for 5 sessions (monthly counter $1.89 to $4.79). Orbit's own estimates (shown in the plan card from 11 to 18 past builds) were $0.52-0.84 (c3), $0.61-1.00 (c4), $0.45-0.74 (e1); actual values fell inside them. Plan brief size in characters was not captured (the dashboard does not show it): T2 must measure it from the stored plan.

### What the screens look like (descriptions, not scores)

- **c1:** the only screen reachable was sign-in (no sign-up page was built, and the dashboard Users tab has no "add user"): flat blue header bar, centered white card, system sans.
- **c2:** first screen "Grade da semana" is a title, a line of text and a red error "Não foi possível carregar as aulas: unauthorized" (a public grid reading a table that needs sign-in). After M2 a light-blue info band and Grade/Entrar links appear; the error stays. Header is plain text on white.
- **c3:** dark background, amber accent, rounded sign-in card; with a throwaway account the list shows a title, an amber primary button, status filter pills, a search field and a plain-text empty state in a dark card.
- **c4:** cream background, brown accent, text tabs in the header, one centered column; the products page is a title, a plain-text empty state and an inline add form.
- **e1:** warm off-white background, forest-green accent, active nav pill, large title, full-width search, plain-text empty state. Larger type and buttons than the others.
- Motion: not assessable from static screenshots; the generated CSS was not inspected. To check in T7 by reading `src/index.css` of each build.

### Cross-brief checks (observed, not scored)

- Palettes differ by hue (blue, indigo-blue, amber on dark, brown on cream, green on off-white), but c1 and c3 colors were dictated by the brief; for the three briefs with no look given (c2, c4, e1) the planner wrote "clean/simple, light" every time.
- Structure repeats: header bar plus one centered narrow card or column, empty states as plain text, no illustration, no data visualization, no hero element. 4 of 5 `Look` texts open with "simple" or "clean".
- Tells from the skill's list (gradient wash, same soft shadow on identical cards, arrow on every button, one headline word in another color): none noticed in the screens seen; a centered white card with a soft shadow appears in c1, c2, c3 and e1 sign-in screens.

### Findings outside the visual rubric

1. **Reply language, confirmed on the real stack.** For pt-BR briefs with the dashboard in English, c1 and c4 produced an English plan and an English app UI; c2 and c3 came out in Portuguese. This is the bug fixed (not yet merged) in `fix/builder-reply-language`; this baseline image does not include the fix.
2. **Look question offers two presets.** In c2 the planner's look question listed "Clean and simple, light background, one accent color", "Dark and energetic, sporty feel" and "Let me decide". That list steers toward the generic result; relevant to DQ-04.
3. **Build failure with a self-inflicted cause (c4, M1).** The agent wrote a scratch file `src/probe.ts` with type errors on purpose to learn the SDK types (it could not open the SDK files), the leftover file failed typecheck, and the turn stopped at the fix-iteration limit ("reached the maximum number of build-fix attempts"). About $0.19 had been spent. Continuing from the product's "Continue" button worked and emptied the file. Not part of this feature; candidate for a separate fix (give the agent a way to read SDK types, or exclude scratch files from the typecheck gate).
4. **c2 ships a first screen with a visible `unauthorized` error** because the public grid reads a table that requires sign-in.

Cross-brief and rubric scores: **not recorded; needs a blind reviewer** (section 2, step 5).

## 4. T2 retest on the real model (2026-11-23)

Image `zeep-orbit:dq-local` = `develop` (`f392c0b`, same as beta.2) + T2 (`91f6481`) and nothing else; only the `zeep` container was swapped, builder, registry proxy, database and settings unchanged (same models, same empty persona). Same protocol as the baseline ("Decide for me" on every question, plan approved as proposed, no follow-up about visuals). To limit cost the builds were stopped early: c2 after 2 of 4 milestones, c4 and e1 after milestone 1 (the first screen and the list were already visible). Cost of the three: about $0.89 (monthly counter $4.79 to $5.68). One sample per brief, so these are observations, not a measured improvement.

### What the planner produced (stored plan `look`, read from the database)

| Brief | direction | type | density | motion | colors |
| --- | --- | --- | --- | --- | --- |
| c2 gym | "Energetic and clean, readable at a glance on a phone at the gym" | system sans, large time and modality labels, compact secondary text | balanced | "Subtle feedback on booking and cancelling only; no decorative animation" | light background, one strong accent for booking buttons, clear status colors |
| c4 bakery | "Warm and practical: cream, toasted brown, red alert for low stock" | system fonts, large readable numbers | balanced | "soft feedback when saving a movement and when a low-stock alert appears; no decorative animation" | cream, toasted brown, red for low stock |
| e1 bookshop | "Warm and practical: cream tones, green accent, quick to scan" | system serif headings, system sans body | balanced | "Light fade on page entry; short confirmation after saving or deleting" | cream background, deep green accent, dark brown text |

- All three plans carried a direction (the tool call is refused without one, so this is by construction). The direction was in English even for pt-BR plans (the language fix is not in this image).
- `density` was "balanced" three times out of three. The direction is closer to a restatement of the palette ("warm, cream, green") than to a distinct design idea, and two of three motion lines still say "no decorative animation": the planner keeps the restraint that the v1 skill teaches.
- The look question changed. Baseline c2 offered "Clean and simple" or "Dark and energetic". Now: c2 "Que clima visual combina com a Academia Central?" with Energético e limpo / Escuro e esportivo / Sóbrio e organizado (still two classic presets); e1 "Warm and bookish: cream paper tones, deep green or brown accents, serif headings" / "Clean and calm" / "Simple and practical" (specific to the business); c4 asked a look question (it did not in the baseline) with a "Warm and practical" and a "Clean and compact" option.

### What the screens look like now (descriptions, not scores)

- **c2 (M1, M2):** white header, orange accent (was indigo), red-bordered error box with an orange button. Same layout as the baseline and the same `unauthorized` error on the first screen: a logic problem the visual direction does not touch.
- **c4 (M1):** toasted-brown header bar with a bread icon, cream page, bordered login card, interface in Portuguese (baseline c4 was English). List screen: title, primary button, empty state as a bordered card in Portuguese. Same structure as the baseline.
- **e1 (M1):** cream "paper" background, serif headings, deep green button, dashed-border empty state with a heading and helper text, light fade on entry stated in the plan. Visibly different from the baseline e1 (sans, off-white, empty state as plain text). The strongest change of the three.
- Screenshots (maintainer's scratchpad, to move): `c2` `screenshot-1795447652252-12.jpg`, `c4` `screenshot-1795447925054-14.jpg` (sign-in) and `screenshot-1795447936210-16.jpg` (list), `e1` `screenshot-1795448124254-17.jpg` (sign-in) and `screenshot-1795448135275-19.jpg` (catalogue), all under `/var/folders/.../claude-chrome-screenshots-4hMJLY/`.

### Skill loads (from `builder_events`, type `skill_loaded`)

| | Sessions where `orbit-frontend-design` was loaded in the first build turn |
| --- | --- |
| Baseline (5 sessions) | 1 of 5 (c4). c1 loaded only `orbit-crud-screens`, c2 only `orbit-forms-and-states`, c3 and e1 none. |
| With T2 (3 sessions) | 2 of 3 (c2, e1); c4 none. |

Three sessions cannot say whether T2 raised the load rate (T2 does not touch skills); it only shows the baseline rate was low, which is the reason for T4.

### Reading

- T2 does what it was built for: every plan now has a direction, type, density and motion, and the look question is more specific. Visible effect on the generated screens: clear in e1, small in c4, none beyond the accent color in c2.
- It does not by itself make the output "designed". The direction stays close to "warm/clean/practical + one accent", and the skill that turns a direction into craft (T5) is still v1, which asks for restraint. Expect the main gain from T4 (always load the skill) and T5 (skill v2), not from T2.
- Not yet scored; needs the blind review (section 2, step 5) with baseline and T2 screenshots shuffled.

## 5. P1 complete: T2 + T3 + T4 + T5 on the real model (T6 and T7), 2026-11-23

Image `zeep-orbit:dq-p1` = `develop` (same as beta.2) + commits `91f6481`, `00de0ff`, `2d89d54`, `392c94e`. Same stack, same models, same empty persona, same protocol as the baseline ("Decide for me" on every question, plan approved as proposed, backend proposals confirmed, no follow-up about visuals). To limit cost each brief was built to **milestone 1 only**; baseline numbers below are the baseline's milestone 1 for a like-for-like comparison. One run per brief: observations, not a statistical result. Scores are not recorded (blind review pending).

### Skill loads (T6)

| | `orbit-frontend-design` loaded in the first build turn |
| --- | --- |
| Baseline (beta.2) | 1 of 5 |
| T2 only (3 sessions) | 2 of 3 |
| **T2 to T5** | **5 of 5** (all loaded version 2) |

Negative case, one sample: on the e1 session a follow-up "Change the text of the Sign out button to Leave." loaded no skill (0 of 1, 4 tool calls). One negative sample proves nothing about false positives; a fixed set of positive and negative turns is still to be run (T6 is open for that, or T14 for automating it).

### What the planner and the build produced

| Brief | `direction` (plan) | M1 cost (baseline M1) | First screen, in words |
| --- | --- | --- | --- |
| c1 bike shop | "Simples, limpo e funcional para balcão de loja: azul e branco, leitura rápida" | $0.1811 ($0.2052) | Blue header and a centered sign-in card, in Portuguese. Same as the baseline: the brief fixed "simples, azul e branco". |
| c2 gym | "Esportivo e organizado: grade clara, cores fortes só nas modalidades" (`ui_language` came out as "en") | $0.1966 ($0.1711) | Brand mark "AC", title "This week at Academia Central", day selector with class counts, class cards with large time, colored modality tags, spots-left bars and a "Full" state, orange accent, fade-and-rise on day change. Baseline: a title and a red `unauthorized` error. |
| c3 repair | "Oficina à noite: escuro, denso e direto, com leitura rápida de status" | $0.3200 ($0.2526) | Split sign-in: graphite panel with diagonal stripes and a large "Mecânica Sul / Pedidos de conserto" with the second line in orange, form on the right. List: header with an orange icon, large title with a count ("0 pedidos"), status filter pills, empty state with heading, help text and a dashed border. |
| c4 bakery | "Warm and practical for a bakery owner checking stock on a phone at the counter" | $0.2551 ($0.3512, includes the failed attempt) | Brown header with an icon, centered brand block (round icon, large name, tagline) over a sign-in card on cream. No sign-up in M1, so the list was not seen. |
| e1 bookshop | "Warm and bookish, calm like a printed catalogue, easy to read at the counter" | $0.2899 ($0.2051) | Split sign-in: deep-green panel with a serif "Bookshop Catalogue", a short rule and a tagline; cream form side. Catalogue: green header with serif brand, large serif "Books", "0 titles in your catalogue", a rule, empty state "Your shelves are empty" with help text in a dashed box. |

- **Cost:** milestone 1 summed over the five briefs: $1.2427 now against $1.1852 in the baseline (+4.8%), and the baseline figure includes the c4 failed attempt. Per brief the change ranges from -27% to +41%, so with one run each this is within noise but suggests a modest increase from the larger prompt and the extra skill load.
- **Look question:** c2 now asks "Which direction fits Academia Central best?" with Energetic and bold (dark, strong accent, big type) / Clean and welcoming / Sporty and structured; e1 asks "Which feel suits your shop?". The classic light-or-dark pair is gone.
- **Motion:** plans now carry motion lines ("light fade-in on page entry", "smooth transition when switching day"); the c2 build shows day-switch animation described by the model, the others describe a light fade. The generated CSS was not inspected.
- **Language:** the English-for-Portuguese problem is still there in this image (c2 `ui_language` "en", e1 in English for an English brief is expected); it is the separate fix on `fix/builder-reply-language`.

### Side effects to check

1. **Sample data in c2.** Because the backend had no tables yet at milestone 1, the model filled the schedule with example classes and showed a banner "Example schedule: these classes are sample data while the real schedule is being set up". It was disclosed, but the core rule says never to fake data pretending to be real; the new skill's "never invent data to fill a screen" line did not prevent it. Needs a look in T5 follow-up or a core wording fix.
2. **Larger first build.** c3 M1 cost 27% more than the baseline M1; watch the cost per milestone before shipping.
3. **Plans have 4 milestones in c1 and c2** (baseline had 3), one of them a finishing pass ("Acabamento"); not caused by this feature but worth watching.

### Reading (T7)

- The effect is large where the baseline was weakest: c2 and e1 are now clearly different from the baseline (layout, hierarchy, typography, state design), c3 changed its structure (split sign-in), c4 is a modest improvement, c1 is unchanged because the user dictated the look.
- Most of the gain came after T4 and T5 (the skill is always loaded, and v2 asks for composition and states). T2 alone changed the plan text and the look question, and visually only e1.
- The earlier risk of a new uniform style is real: two of five first screens became a split panel with a brand block. Distinctiveness across project types should be checked in the blind review, with more than one run per brief.
- Success criteria from the spec: "blind comparison, reviewer prefers the new output in a clear majority" is **not yet checked** (needs a reviewer who did not run the sessions); "no two project types share palette and type" holds by hue and typography for these five. P1 is therefore implemented and measured informally, and not yet accepted.

Screenshots (maintainer's scratchpad, to move): c1 `...-1795451664390-21.jpg`; c2 `-22`, `-23`, `-24`; c3 `-25` (sign-in), `-27` (list); c4 `-28`, `-29`; e1 `-31` (sign-in), `-32` (catalogue); all `screenshot-17954…` under `/var/folders/…/claude-chrome-screenshots-4hMJLY/`.

### Sample-data fix, verified once (2026-11-23)

Change: the core rule, `orbit-crud-screens` (now version 2) and `orbit-frontend-design` (now version 3) forbid sample, example or placeholder rows even with a notice; the old wording only forbade data "pretending to be real". Image `zeep-orbit:dq-p1b`. Re-run of c2 with the same protocol, M1 only (session `ada0248d-b096-4f68-be8d-8d1aac21da6f`, $0.2727):

- This time the planner asked no questions and went straight to a plan; direction "Clara e energética para uma academia: legível em celular, na porta da academia", `ui_language` pt-BR (the language came out right in this run).
- The build proposed the backend change first (the behavior the rule asks for), then built the screen: **no sample rows**. The first screen is "GRADE DA SEMANA" in heavy condensed capitals, a black bar with an orange underline, a date selector with "HOJE", and the model's note that the empty state appears when a day has no classes.
- The screen shows "NÃO FOI POSSÍVEL CARREGAR A GRADE / unauthorized" instead of the empty state: the public grid reads a table that requires sign-in. That is the same logic problem as the baseline and the first P1 run (finding 4 in section 3), now visible in all three runs. It is separate from this fix: the plan says students see the grid without signing in, while the table is created with access for signed-in users only. Candidate for a separate fix (a planning or backend-proposal rule: a table read by a public screen must be public or the screen must sign in first).
- One run: it shows the wording no longer produces labeled sample data in the case that did before, not that it never will.

## 6. Kit eval (T10), spike (T12)

Not run yet.

## 6. Fix for screens of restricted tables (PR #58), measured once on the real model

Image `zeep-orbit:dq-p1c` = P1 + reply-language fix + the #58 rule, same stack and models as before, "Decide for me" on every question, backend proposals confirmed, stopped after milestone 1. One run per brief, so this is an observation, not a rate.

| Run | Brief | What the plan said | First screen after M1 | M1 cost |
| --- | --- | --- | --- | --- |
| A | c2 as written | `classes` any account; sign-in flow first | Sign-in screen: black brand panel with orange accent, "Sua aula, sua vaga.", form on the right. No error. | $0.3856 (estimate $0.19-$0.25) |
| B | c2 rewritten so the grid is the first screen but the table is not public | `classes` any account, "Visitante sem login não lê nada"; first screen "Grade semanal (inicial)" | Empty weekly grid with a banner "Entre para ver as aulas" and a button to sign in or create an account. No error. | $0.3876 |

- **Run A does not test the rule.** The plan already started on sign-in, so the baseline failure (public grid reading a signed-in table) could not happen.
- **Run B is the baseline condition** (grid first, table closed to visitors) and the app handled it: empty grid and an invitation to sign in instead of a red `unauthorized`. In B the planner also caught the conflict at planning time, asking how the grid should appear before login and offering only safe options, so part of the result may be the planner and not the #58 rule. The run cannot separate the two.
- **Not tested:** the exact baseline sequence (plan says visitors see the grid, table `signed_in`, no question asked). One run, M1 only; the signed-in screens were not opened.
- **Cost:** M1 came out above the estimate shown by Orbit in both runs ($0.3856 and $0.3876 against $0.19-$0.26).
- Screenshots: run A `screenshot-1795463339272-34.jpg`, run B `screenshot-1795463630080-35.jpg` (Chrome screenshots folder, not in the repo).

## 7. Blind review, first pass (one reviewer, 7 pairs)

Method: previews cropped from the saved screenshots (chat panel removed), baseline and P1 sorted into A/B at random per pair, one reviewer (the maintainer) chose the better side or a tie. Rubric scores (1-3) were not given, only the preference. The key was read after the answers.

| Pair | Preference |
| --- | --- |
| c2 weekly grid | P1 (confounded: baseline shows a red `unauthorized`, P1 shows labeled sample rows, both since fixed) |
| c3 sign-in | P1 |
| e1 catalogue | baseline |
| c1 sign-in | baseline (brief fixed "simples, azul e branco"; baseline image had a higher resolution) |
| c4 sign-in | P1 |
| c3 order list | baseline (same resolution on both sides) |
| c4 products | tie |

Totals: P1 3, baseline 3, tie 1. Without the confounded c2 pair: P1 2, baseline 3, tie 1.

- **Reading:** no visual improvement is shown by this sample. What is shown is that the skill is loaded (1 of 5 to 5 of 5). One reviewer, one run per brief, mixed screen types and image resolutions: it does not show that P1 is worse either.
- **Correction:** section 5 describes the change as large for c2 and e1. The blind pass preferred the baseline for e1; that wording was an unblinded impression.
- **Open:** the reviewer's reason for the three baseline preferences (e1, c1, c3 list) is needed to decide between adjusting the skill and moving to P2; a second draw of the five briefs on `dq-p1c` (about $2) would add samples.

## 8. Blind review, second pass (one reviewer, 6 pairs, equal resolution)

Method: same as section 7, with two changes. Every image was resampled to the same width (940 px) before cropping, so resolution cannot tell the sides apart. The P1 side is image `dq-p1c` (P1 plus the #56 language fix and the #58 gating fix), a fresh run of each brief; the baseline side is the section 3 capture. c2 was left out (confounded, see section 7). Same reviewer as section 7, so this is not an independent second opinion. The key was read after the answers.

| Pair | Preference |
| --- | --- |
| c1 sign-in | P1 |
| e1 catalogue | P1 |
| c3 sign-in | P1 |
| c3 order list | P1 |
| c4 products | P1 |
| c4 sign-in | P1 |

Totals: P1 6, baseline 0, tie 0.

- **Reading:** with resolution equalized and a fresh run per brief, the reviewer preferred the P1 result in every pair. Section 7 (P1 3, baseline 3, tie 1) is not reproduced. The likely differences are the resolution mismatch in section 7 and the #58 fix and language fix now in the image; this was not isolated, so the cause is a hypothesis.
- **Limits:** one reviewer who knows the project, one run per brief, five briefs, and the baseline is a single older run. A 6-0 result from one person is a strong signal for this sample, not a measured effect size. The same reviewer's section 7 verdicts on c1, c3 list and e1 went the other way, so the preference is not stable across runs of the same brief.
- **Cost (M1 counters on the screen):** c1 $0.2233, c3 $0.3639, c4 $0.2733, e1 $0.1835, about $1.04 for the four runs; c2 run A was $0.3856 earlier.
- **Decision input:** this removes the main reason to adjust skill v3 before P2. It does not replace an independent reviewer or the P3 visual check. Moving to P2 stays Julio's call.

## 9. T8: default template with Tailwind and the Orbit kit (measured 2026-11-23)

Decisions (design §3.2, left open there):
- **Tailwind 4.3.5 with `@tailwindcss/vite` 4.3.5**, both exact and in `devDependencies` (like Vite). CSS-first theme: the tokens are plain `:root` variables, mapped to utilities with `@theme inline`, so the "tokens first" rule edits one file.
- **No component library and no icon package.** The 14 kit pieces (Button, Label, Input, Textarea, Select, Checkbox, FormField, Card, Badge, Table, Dialog, Tabs, Toast, EmptyState, Skeleton) are hand-written, with the native `<select>` and `<dialog>` for keyboard and focus behavior. Icons stay inline SVG, as the skill already says. The only added packages are Tailwind and its Vite plugin plus their transitive dependencies; no Radix, no `lucide-react`, no `clsx`, no `tailwind-merge` (a 3-line `cn` helper instead).
- Reason: each added package is supply-chain surface (AGENTS.md §9 spirit) and the native elements cover the accessibility needs of a first kit. The cost is that Select and Dialog are less styleable than a headless library; revisit if a brief needs a custom listbox.

Measurements, same machine, warm npm cache:

| | Old template | New template |
| --- | --- | --- |
| `npm ci` | 1.9 s | about 4 s (node 22 bookworm container, cold install) |
| `npm run build` | 1.8 s | 2.0 s on the host; 4 s `tsc` + `vite build` in the container |
| `node_modules` | 108 MB | 143 MB in the container (130 MB on the host) |
| CSS in `dist` | 0.74 kB | 13.3 kB (3.75 kB gzip) |
| JS in `dist` | 265 kB | 267 kB |

The two columns are not an identical setup (host warm cache versus a fresh container), so the install times are indicative, not a benchmark. The lockfile carries the native binaries for linux glibc and musl, arm64 and x64, and the install and build ran green in `node:22-bookworm-slim`, the base of `Dockerfile.builder`. `npm run lint` and `npx tsc -b` are green. `vite build --base ./` keeps the relative asset paths the preview needs.

Not yet done for T8: a built session on the local stack previewing under the token path with the new template (needs an image build), and a visual check of the new default screens.

Found while doing it, left for T9 because they change prompts and skills: `core.md` says the template uses "plain CSS", and the skills `orbit-react-patterns` and `orbit-responsive-layout` say "plain CSS" or "no framework". Both are wrong for the default template now, and wrong-for-some-templates if they only say Tailwind, so the wording should become template-neutral ("follow the template guide").

### 9.1 Decision change after web research (same day)

The first T8 cut used native `<select>` and `<dialog>` and no icon package. After checking the options (shadcn/ui changelog of 2026-07-02 makes Base UI its default and says Radix stays supported; npm facts read on 2026-11-23), the maintainer chose to use libraries for the interactive parts:

- **`@base-ui/react` 1.9.0** (MIT, React 17-19 peer, 5 direct dependencies) behind the kit's `Dialog`, `Select` and `Tabs`. The agent imports only from `components/ui`, so the library stays an implementation detail.
- **`lucide-react` 1.78.0** (ISC, no dependencies) for icons, named imports only.
- Not chosen: Radix (many small packages, ecosystem moving to Base UI), React Aria Components (6.9 MB unpacked), Phosphor (last npm release 2025-05), HeroUI/MUI/Chakra (ship their own look, which conflicts with the tokens-first rule).

Measured in a temp copy before adopting: Dialog + Select + Tabs + 5 icons added +86 kB JS (+29 kB gzip) and `node_modules` went from 143 MB to 205 MB (+62 MB, 51 MB of it the Lucide package on disk; only used icons are bundled). In the committed default template the home page imports none of them, so the production build is unchanged at 266.7 kB JS (84.7 kB gzip) and 16.65 kB CSS; an app pays only for what its screens import.

Checks after the change: `npm run lint`, `npx tsc -b`, `vite build --base ./` and `npm ci --dry-run` (lockfile in sync) are green. **Not verified yet:** the real behavior of the three Base UI components in a browser (open, keyboard, focus, positioning under the relative base) and in a builder session; the browser tool did not respond when tried.

### 9.2 Browser check of the kit (2026-11-23, Chrome, demo page outside the repo)

Dialog, Select, Tabs, Toast and the empty state were exercised on a throwaway page built from the template (all five imported, so the library code was in the bundle):

- **Dialog:** opens centered over a dimmed backdrop, focuses its first field, closes with Escape, and focus goes back to the button that opened it.
- **Select:** opens, ArrowDown plus Enter picks an option and the controlled value updates; clicking the label focuses the trigger (same `id`).
- **Toast:** appears bottom right with the success color.
- **Two changes found by looking:** (1) arrow keys only moved focus between tabs, Base UI's default being manual activation, so `Tabs` now sets `activateOnFocus`; (2) the Select list opened over the trigger and wider than it (Base UI aligns the selected item with the trigger), so it now uses `alignItemWithTrigger={false}` and opens below at the trigger's width.
- Not checked: a screen reader, Firefox and Safari, a phone-width viewport, and the kit inside a real builder session (needs the image build; next step).
