# Orbit site template - guide for the AI agent

This project is a static site (landing page, one-pager, portfolio, menu, event
page). It has **no backend**: no API, no database, no sign-in, no server code.
Do not add a data layer, an SDK or a login to it.

## Stack
Vite 7, React 19, TypeScript (strict), Tailwind CSS 4. A small UI kit
(`orbit-ui`) is in `src/components/ui`: use its components first and create a new
one only when the kit has none. Its API is in `UI_KIT.md`. The design tokens
(colors, type, radius, shadow, motion) are CSS variables at the top of
`src/index.css`: change the look there, not inside components.

## Layout
- `src/main.tsx` - entry.
- `src/App.tsx` - the page: header, sections, footer. Add a section by creating a file in `src/sections/` and listing it here.
- `src/sections/` - one file per section (`HeroSection.tsx`). Give each section an `id`; the header links to it with `href="#id"`.
- `src/components/ui/` - the UI kit (see `UI_KIT.md`). Import from `../components/ui`.
- `src/components/` - your own reusable UI.
- `public/` - files copied as they are (favicon, images). Reference them with a relative path (`logo.png`), never `/logo.png`.
- `index.html` - set `<title>`, the description and the Open Graph tags for the real site; keep `lang` right.
- `vite.config.ts`, `tsconfig*.json`, `eslint.config.js`, `package*.json`, `orbit-template.json`, `TEMPLATE_VERSION` - build plumbing; leave them alone unless the task requires it.

## Rules
- One page with anchor sections is the default. Add `react-router-dom` with `HashRouter` only if the user asks for separate pages; then in-page anchors need `scrollIntoView` instead of `href="#id"`.
- Contact: a `mailto:`, `tel:` or messenger link. A form that submits needs a server, which this project does not have: if the user needs one (orders, bookings, sign-ups saved somewhere), tell them it needs a backend project instead of faking it.
- The preview runs under a changing sub-path (the build base is relative): import assets from `src/` or use relative paths, never absolute `/something` URLs.
- Never hardcode secrets, tokens or API URLs. Never call `localStorage` or `sessionStorage` without a try/catch (the preview iframe blocks them).
- Images: give every image `alt` text and width/height (or an aspect ratio) so the layout does not jump. Prefer a few well-chosen images over many.
- Accessibility: one `<h1>`, heading levels in order, links and buttons you can reach with the keyboard, visible focus, enough contrast.
- No new dependencies unless the task cannot be done without them. Tailwind, the kit (built on `@base-ui/react`) and `lucide-react` for icons are already installed; do not add another component library or icon package.
- Text files only. Do not touch `node_modules`, `dist`, `.git`.
- Keep `npm run lint` and `npx tsc -b` green before finishing a turn.
- UI copy follows the user's language; code, identifiers and comments in English.
