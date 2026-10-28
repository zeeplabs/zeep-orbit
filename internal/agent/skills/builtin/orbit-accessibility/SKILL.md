---
name: orbit-accessibility
description: Use when building interactive UI, forms, dialogs, menus or navigation: semantic HTML, accessible names, focus, keyboard support, contrast and error announcements.
license: MIT
version: 1
source: Adapted from the fixing-accessibility skill in ibelick/ui-skills (MIT, Copyright Julien Thibeaut), rewritten for generated React code
---

# Accessibility

Apply these rules while you write code, so the screen works with a keyboard and a screen reader the first time. Prefer small, targeted changes: do not refactor unrelated code, and do not add ARIA when a native element already gives the behavior.

## Priorities

Fix in this order: accessible names, keyboard access, focus and dialogs, semantics, forms and errors, announcements, contrast and states, media and motion.

## 1. Accessible names (critical)

- Every interactive control has a name. An icon-only button needs `aria-label` (in the user's language), and the icon gets `aria-hidden="true"`.
- Every input, select and textarea has a visible `<label>` tied with `htmlFor` and `id`. A placeholder is not a label.
- Link text says where it goes. No "click here".
- Decorative images and icons use `alt=""` or `aria-hidden="true"`; meaningful images get real alt text.

```tsx
<button type="button" aria-label="Close" onClick={onClose}>
  <CloseIcon aria-hidden="true" />
</button>
```

## 2. Keyboard access (critical)

- Use `<button>` for actions and `<a>` / `<Link>` for navigation. Never put `onClick` on a `<div>` or `<span>`. If you truly must, add `role`, `tabIndex={0}` and Enter/Space handling, which a native button gives for free.
- Everything clickable must be reachable with Tab, in a logical order. Never use `tabIndex` above 0.
- Keep a visible focus style. Do not write `outline: none` without a replacement such as `:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }`.
- Escape closes dialogs, menus and popovers.
- Anything that appears on hover must also appear on focus.

## 3. Focus and dialogs (critical)

- A modal moves focus inside when it opens, keeps focus inside while open, and returns it to the trigger on close.
- Prefer the native `<dialog>` element with `showModal()`, which handles most of this. Give it a heading and `aria-labelledby`.
- Opening a dialog must not scroll the page unexpectedly, and the content behind it must not be tabbable.
- For complex widgets (combobox, menu, date picker), use an established accessible primitive already in the project instead of custom behavior. Do not add a dependency for this unless the task needs it.

## 4. Semantics (high)

- Use landmarks: one `<main>`, `<header>`, `<nav>` with an `aria-label` when there is more than one.
- Headings go in order (one `h1`, then `h2`, no skipped levels) and describe the content, not the styling.
- Lists use `<ul>` or `<ol>` with `<li>`. Data tables use `<table>`, `<th scope="col">` and a `<caption>` or `aria-label`.
- If you use a `role`, include its required attributes. Do not add a `role` to an element that already has the native semantics.

## 5. Forms and errors (high)

- Link an error message to its field with `aria-describedby` and set `aria-invalid="true"` while it is invalid.
- Mark required fields with `required` and say so in the label text, not only with a color or an asterisk.
- Helper text is also tied with `aria-describedby`.
- A disabled submit button should say why, in text next to it, or stay enabled and report what is missing on submit.
- After a failed submit, move focus to the first invalid field or to an error summary.

```tsx
<label htmlFor="email">Email</label>
<input id="email" type="email" required aria-invalid={Boolean(error)} aria-describedby={error ? 'email-error' : undefined} />
{error && <p id="email-error" role="alert">{error}</p>}
```

## 6. Announcements (medium-high)

- Important form errors and results use `role="alert"` or an `aria-live="polite"` region that exists before the content changes.
- Loading uses `aria-busy="true"` on the region or visible status text such as "Loading invoices".
- A toast must never be the only place a critical message appears.
- Expandable controls use `aria-expanded` and `aria-controls`.

## 7. Contrast and states (medium)

- Text 4.5 to 1, large text and icon-only controls 3 to 1, against the real background.
- Disabled, selected and error states cannot depend on color alone.
- Touch targets are at least 44 by 44 CSS pixels where possible.

## 8. Media and motion (low-medium)

- Respect `prefers-reduced-motion` for non-essential animation.
- No autoplaying media with sound. Provide captions for speech in video when you add video.

## Reviewing existing code

When asked to review a file, report each problem with the exact snippet, one short sentence on why it matters, and a concrete fix. Fix critical items first.
