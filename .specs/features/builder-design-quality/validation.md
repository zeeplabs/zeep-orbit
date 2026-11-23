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

## 4. After P1 (T7), skill load rates (T6), kit eval (T10), spike (T12)

Not run yet.
