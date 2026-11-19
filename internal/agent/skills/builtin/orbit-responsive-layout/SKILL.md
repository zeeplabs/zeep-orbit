---
name: orbit-responsive-layout
description: Use when laying out pages for phone, tablet and desktop: mobile-first CSS, grid and flex, breakpoints, wide tables, navigation and touch targets, without horizontal scrolling.
version: 1
source: Written for the Orbit builder (Zeep Orbit); plain CSS only, no framework assumed
---

# Responsive layout

The template uses plain CSS in `src/index.css` and a centered `.app-main` container. You cannot look at the result, so build layouts that are hard to break: flexible by default, with few fixed sizes and few breakpoints.

## Rules of thumb

- Write for a narrow screen first, then add rules for wider ones with `@media (min-width: ...)`. Two or three breakpoints are enough, for example 40rem, 64rem and, if needed, 80rem. Use the tokens you already have for spacing.
- Keep the document viewport tag in `index.html` as it is. Do not disable zoom.
- Use `rem`, `%`, `fr`, `ch` and `clamp()` instead of fixed pixel widths. A fixed `width` in pixels on a container is almost always a bug. Use `max-width` plus `width: 100%`.
- Never use a fixed `height` on content. Use `min-height`, and `100dvh` instead of `100vh` for full-screen shells so mobile browser bars do not cut content.
- Images and media: `max-width: 100%; height: auto;`, and set `width` and `height` attributes to avoid layout jumps.
- Text scales: body about 1rem, fluid headings with `clamp(1.5rem, 4vw, 2.25rem)`.

## Grid and flex

- Card grids: `display: grid; grid-template-columns: repeat(auto-fit, minmax(16rem, 1fr)); gap: 1rem;` adapts without any breakpoint.
- Page shells with a sidebar: stack on narrow screens, then switch to two columns at the first breakpoint (`grid-template-columns: 14rem 1fr`).
- In flex or grid children that hold long text, add `min-width: 0` so they can shrink instead of overflowing, and `overflow-wrap: anywhere` for long words and emails.
- Use `gap` for spacing between items, not margins on every child.
- Toolbars and button groups: `display: flex; flex-wrap: wrap; gap: 0.5rem;`.

## Tables and wide content

- A wide table goes inside a wrapper with `overflow-x: auto`, and the wrapper gets `tabindex="0"` and an accessible label so it can be scrolled with the keyboard.
- For lists of records, prefer stacked cards on a phone and a table from the first breakpoint up: render the same data twice only if it stays simple, otherwise use the scroll wrapper.
- Keep the most important column first, and right-align numbers.
- Long code, URLs or ids: `overflow-wrap: anywhere` or a horizontal scroll area, never a wider page.

## Navigation

- Few links: let them wrap, or stack them. Many links: show a "Menu" button below the first breakpoint that toggles the list (a real `<button>` with `aria-expanded` and `aria-controls`), and show the list inline above it.
- Keep the primary action reachable without scrolling on a phone.

## Touch and input

- Interactive targets at least 44 by 44 CSS pixels, with space between neighbours.
- Inputs use at least 1rem font size so phones do not zoom on focus. Set the right `type` and `inputMode` (email, tel, numeric).
- Do not rely on hover for anything essential.

## Check your work

- Search your CSS for fixed widths, `100vw` (it includes the scrollbar and causes horizontal scroll, use `100%`), and negative margins that can push content out.
- Every flex row that can get long needs `flex-wrap` or `min-width: 0`.
- Run `run_build` with step `build` and fix what it reports.
