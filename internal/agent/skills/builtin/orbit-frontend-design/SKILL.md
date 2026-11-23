---
name: orbit-frontend-design
description: Use for any new screen or restyle: turn the brief's Look into CSS tokens, type, layout, states and purposeful motion, avoiding the generic AI look. Yields to company standards and the template guide.
license: Apache-2.0
version: 3
source: Adapted from the frontend-design skill in anthropics/skills (Apache-2.0), rewritten for the Orbit builder
---

# Frontend design

Use this for every new screen and every restyle, not only when the user says the app looks plain. The default result of a model asked for "a customer list" is a white page, one accent color, a centered narrow column and a plain-text empty state. Your job is to build something that looks made for this app and its users. It never overrides the company standards or the template guide: if either defines a design system, tokens, components or a brand, follow that and use this skill only for what it leaves open.

## 1. Decide the direction first

The APP BRIEF has a `Look` line: direction, type, density, motion, plus the user's own style and colors. Build that. Do not reinterpret it as "clean and simple". If the user asked for a style or colors, those win over your taste.

Without a brief, or with a `Look` that has no direction, choose one now from who uses the app, where and for what, and say it to the user in one sentence. Pick from this range or invent a better fit:

| Direction | Type | Shape and density | Color | Motion |
| --- | --- | --- | --- | --- |
| Editorial, bookish, calm | serif headings, sans body, generous leading | airy, hairline borders, small radius | paper-like neutrals, one deep ink accent | slow fades, nothing bouncy |
| Dense, operational | sans with tabular numbers, small sizes, strong weight contrast | compact rows, small radius, clear dividers | high contrast, status colors carry meaning | almost none, instant feedback |
| Friendly, playful | rounded sans, large friendly headings | big targets, large radius, roomy | warm neutrals, one saturated color, a second for rewards | short springy presses, small celebrations |
| Calm, clinical | humanist sans, large body text | airy, soft edges | low-saturation neutrals tinted toward the accent | quiet fades only |
| Bold, energetic | very large headline against small body, heavy weights | strong blocks, big numbers, tight grid | saturated accent block on a neutral base | confident entrances, crisp transitions |
| Dark, technical | sans for UI, monospace for data | layered dark surfaces, thin borders | dark neutrals tinted toward the accent, one bright accent | restrained, quick |

Pick the row by the job of the screen, not by habit. A gym, a clinic reception, a children's game and a stock sheet should not share a direction.

## 2. Write the tokens before any component

Read `src/index.css` with `read_file`, then extend its `:root` variables. Do not scatter hex values through components.

- Color: background, surface, raised surface, text, muted text, border, accent, accent text, plus error and success when a screen needs them. Tint the neutrals toward the accent hue (a slightly warm or cool gray) instead of pure gray. One accent, used for the primary action and the few things that matter.
- Type: two stacks at most, built from system fonts, never an external font host. Define a scale (for example 0.8125, 0.9375, 1.125, 1.5, 2.25, 3 rem) and use only those sizes.
- Space and shape: a 4 or 8 px scale; one radius for controls, optionally one larger for surfaces; one shadow level, used sparingly.
- Motion: tokens for duration and easing, so every transition in the app agrees.

System stacks that give character without loading anything:

```css
:root {
  --font-serif: 'Iowan Old Style', 'Palatino Linotype', Palatino, Georgia, serif;
  --font-humanist: Seravek, 'Gill Sans Nova', Ubuntu, Calibri, 'DejaVu Sans', sans-serif;
  --font-rounded: ui-rounded, 'Hiragino Maru Gothic ProN', Quicksand, Nunito, system-ui, sans-serif;
  --font-geometric: Avenir, 'Avenir Next', Montserrat, 'Century Gothic', system-ui, sans-serif;
  --font-mono: ui-monospace, 'Cascadia Code', 'Source Code Pro', Menlo, Consolas, monospace;
  --ease-out: cubic-bezier(0.22, 1, 0.36, 1);
  --dur-fast: 120ms;
  --dur-base: 220ms;
}
.num { font-variant-numeric: tabular-nums; }
```

## 3. Compose with intent

- One memorable element per app, and keep the rest quiet: a headline treatment, a stat strip with large numbers, a color-coded status system, a brand mark built from the app's name, a strong header. Decide what it is and put it where the user's eye lands first.
- Do not put every screen in one centered narrow card. A list screen gets a header with the title, a count and the primary action; a toolbar row for search and filters; then the content. A sign-in screen carries the app's identity (a brand block, a side panel, a large heading), not only a form.
- Make hierarchy visible: the most important thing is the largest or the most contrasted, and whitespace between groups is clearly larger than inside a group.
- Numbers, dates, money: right-align numeric columns, use tabular numbers, keep units next to the value.
- Icons: small inline SVG drawn in the component, consistent stroke width. An emoji is not an icon system.
- Left-align text blocks, keep reading lines under about 75 characters.

## 4. Design every state, in the same look

Loading, empty, error and success are screens too.

- Loading: a skeleton of the real layout, not a bare "Loading...". A very light shimmer is fine.
- Empty: a short heading, one line that says what will appear here, and the primary action. Never only a plain sentence. This is also what a screen shows before its data exists: never fill it with example or sample rows, even with a notice, because the preview then looks finished while the data is not real.
- Error: what happened, what to do, a retry button. Keep what the user typed.
- Success: a confirmation that uses the same word as the action ("Changes saved"), visible for a moment.

## 5. Motion with a purpose

Motion is on by default where it explains something, and absent where it only decorates.

- Page entry: opacity from 0 and a small rise (about 8 px), 180 to 260 ms, ease-out. One entrance per page, not one per section.
- Lists: a staggered entrance for at most the first 6 items, 30 to 50 ms apart; never stagger a long list.
- State changes and user actions: a pressed button (scale about 0.98, 100 to 140 ms), a row that appears or disappears, a status that changes color, a toast that slides in. This is where motion is most valuable.
- Only animate `transform` and `opacity`. No infinite loops, no parallax, no animation on scroll.
- Wrap everything non-essential so it respects the user's setting:

```css
@media (prefers-reduced-motion: no-preference) {
  .page { animation: rise var(--dur-base) var(--ease-out) both; }
}
@keyframes rise { from { opacity: 0; transform: translateY(8px); } }
```

The `Look` of the brief says where motion goes. Follow it; if it says none, add none.

## 6. Avoid the tells

Do not ship these unless the brief points there: a white page with one blue or purple accent and nothing else; a gradient wash as decoration; glassy translucent cards; identical rounded cards with the same soft shadow everywhere; a middle-dot meta string; one word of a headline in another color or italic; all-caps tracked labels above every heading; an arrow on every button; "Welcome back" above a centered card; a cream background with a serif and a clay accent when the app is not editorial.

## 7. Quality floor

Check contrast: normal text at least 4.5 to 1, large text and icons at least 3 to 1, and never rely on color alone for a state. Keep a visible keyboard focus. Make it work at phone width with no horizontal scroll. Do not link to external fonts, icons or images: the preview and the published app must not call third-party hosts. Dark mode only when asked, driven by the same variables.

Write UI text in the user's language, name things the way the user does, and never invent data to fill a screen, not even labeled as example data. You cannot see the result, so keep the CSS simple enough to reason about: a few well-named classes over deep selectors, and watch specificity so one rule does not cancel another. After changing styles run `run_build` with step `build` and fix what it reports.
