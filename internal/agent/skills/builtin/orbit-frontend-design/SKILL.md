---
name: orbit-frontend-design
description: Use when designing or restyling screens: visual hierarchy, spacing and type scale, CSS-variable tokens, avoiding the generic AI look. Yields to the company design system and the template guide.
license: Apache-2.0
version: 1
source: Adapted from the frontend-design skill in anthropics/skills (Apache-2.0), modified for the Orbit builder
---

# Frontend design

Use this when the user asks for a new screen, a restyle, or says the app looks plain or generic. It is guidance for taste. It never overrides the company standards or the template guide: if either defines a design system, tokens, components or a brand, follow that and use this skill only for the parts it leaves open.

## Decide before you code

Spend a minute on the brief, then write the decisions down as CSS variables, not as scattered values.

- Subject, audience and primary job of the screen. A clinic's front desk tool and a children's game need different answers. If the request does not say, pick one concrete reading, state it in one sentence to the user, and build for it.
- Tokens, in `src/index.css`: read the file first with `read_file`, then extend the existing `:root` variables instead of hardcoding colors in components.
  - Color: 4 to 6 named values (background, text, muted text, border, accent, plus error and success if the screen needs them). Keep one accent.
  - Type: one family, or two that are clearly different. Define a small scale (for example 0.875, 1, 1.25, 1.5, 2 rem) and use it everywhere.
  - Space: a scale of 4 or 8 px steps (0.25, 0.5, 1, 1.5, 2, 3 rem). Reuse it for padding, gaps and margins.
  - Shape: one radius for controls, optionally one larger for surfaces. Do not give every element the same radius and the same shadow.
- Spend the boldness in one place: one memorable element (a headline treatment, a number, a color block). Keep everything around it quiet.

## Hierarchy and layout

- Make the most important thing the biggest or the most contrasted, and let size, weight and whitespace do the ranking. Borders, dividers and labels must carry information, not decoration.
- Group related things closer than unrelated things. Whitespace between groups should be visibly larger than inside a group.
- Align to a clear edge. Left-align text blocks; center only short, isolated content.
- Keep line length under about 75 characters for reading text.
- Numbers, dates and money: right-align numeric table columns, use consistent decimals, and keep units next to the value.

## Typography

- Without a project font, use a system font stack (the template already does). Do not link to external font or icon hosts: the preview and the published app should not call third-party hosts, and the build must not need them.
- Use at most three sizes in a content area plus the headline. Use weight, not only size, to separate levels.
- Avoid the usual tells: one word of a headline in a different color or italic, all-caps tracked labels above every heading, middle-dot meta strings, an arrow appended to every button.

## Color and surfaces

- Check contrast: normal text at least 4.5 to 1 against its background, large text and icons at least 3 to 1. Never rely on color alone to show a state; add text or an icon.
- Avoid the defaults that appear on every generated page: a cream background with a serif and a clay accent, near-black with one acid accent, identical rounded cards with the same soft shadow, gradient washes as decoration. Use them only when the brief points there.
- Dark mode is only worth adding when asked. If you add it, drive it from the same variables.

## Motion

- Do not add entrance animations to every section or hover effects to every card. One deliberate moment is enough, and motion that answers a user action (opening, saving, removing) is welcome.
- Wrap non-essential motion in `@media (prefers-reduced-motion: no-preference)`.

## Copy inside the app

- Name things the way the user does, in sentence case. A button says what it does ("Save changes", not "Submit") and the confirmation uses the same word ("Changes saved").
- Errors say what happened and how to fix it. An empty screen invites the next action. Never invent data to fill a screen: use the real schema, or say that nothing exists yet.
- Write UI text in the user's language; keep identifiers and code comments in English.

## Quality floor

Build to this floor without announcing it: works on a phone width, visible keyboard focus, readable contrast, no horizontal scrolling. After changing styles, run `run_build` with step `build` and fix what it reports. You cannot see the result, so keep the CSS simple enough to reason about: prefer a few well-named classes over deep selectors, and watch specificity so one rule does not silently cancel another.
