# orbit-ui kit

Components live in `src/components/ui`; import them from `../components/ui` (or `../../components/ui`).
They are styled with Tailwind utilities that read the tokens in `src/index.css`, so changing a token
restyles every component. Do not edit a component to change the app's identity: edit the tokens.

## Tokens (`:root` in `src/index.css`)

| Group | Variables |
| --- | --- |
| Color | `--background`, `--surface`, `--surface-raised`, `--foreground`, `--muted-foreground`, `--border`, `--primary`, `--primary-foreground`, `--danger`, `--success`, `--warning`, `--ring` |
| Type | `--font-sans`, `--font-heading`, `--font-mono` (system stacks only; no external font host) |
| Shape | `--radius`, `--radius-lg`, `--shadow` |
| Motion | `--ease-out`, `--dur-fast`, `--dur-base`; the `.page` class gives one entrance per page and respects `prefers-reduced-motion` |

Tailwind utilities map to the same names: `bg-primary`, `text-muted-foreground`, `border-border`, `rounded-md`, `rounded-lg`, `shadow-card`, `font-heading`.
Add a token the same way: a variable in `:root` and a `--color-*` line in `@theme inline`.

## Components

| Component | Props | Notes |
| --- | --- | --- |
| `Button` | `variant` primary, secondary, ghost, danger; `size` sm, md; native button props | `type="button"` by default; pass `type="submit"` in forms |
| `Label`, `Input`, `Textarea`, `Select`, `Checkbox` | native props | `Select` is a native select; `aria-invalid="true"` shows the error border |
| `FormField` | `id`, `label`, `hint?`, `error?`, children | Wraps label and control; the control needs the same `id` |
| `Card` | div props | Surface with border and shadow |
| `Badge` | `tone` neutral, success, warning, danger | Put the status in the text |
| `Table`, `Th`, `Td` | `numeric?` on `Th` and `Td` | `Table` scrolls sideways on narrow screens; `numeric` right-aligns with tabular numbers |
| `Dialog` | `open`, `onClose`, `title`, children | Native `<dialog>`: focus trap and Escape included |
| `Tabs` | `items: {id, label, content}[]`, `initial?` | |
| `ToastProvider`, `useToast` | `useToast()(message, tone?)` | Provider is already in `main.tsx`; tone is `success` (default) or `error` |
| `EmptyState` | `title`, `description`, `action?` | Use it for every empty list, with the primary action |
| `Skeleton` | div props | Size with `className`, for example `h-4 w-40` |

## Rules

- Use the kit for buttons, fields, tables, dialogs, tabs and states before writing your own.
- A new component goes in `src/components/` and reads the tokens, never a hardcoded hex.
- Icons are small inline SVG with one stroke width; no icon package.
- Keep a visible focus ring and 4.5 to 1 text contrast when you change the tokens.
