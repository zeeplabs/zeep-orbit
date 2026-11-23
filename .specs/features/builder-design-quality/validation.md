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

Not run yet.

| Brief | Model | Image tag | Plan brief chars | Skill loaded? | Hierarchy | Distinctiveness | States | Contrast | Motion | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| c1 | | | | | | | | | | |
| c2 | | | | | | | | | | |
| c3 | | | | | | | | | | |
| c4 | | | | | | | | | | |
| e1 | | | | | | | | | | |

Cross-brief: not recorded.

## 4. After P1 (T7), skill load rates (T6), kit eval (T10), spike (T12)

Not run yet.
