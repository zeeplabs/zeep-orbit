---
name: orbit-forms-and-states
description: Use when building forms or any async view: validation, submit feedback, loading, empty and error states, retries, and confirmation before destructive actions.
version: 1
source: Written for the Orbit builder (Zeep Orbit)
---

# Forms and states

Every screen that talks to the backend has more than one state. Build all of them on purpose, in the user's language, and never leave a blank area or a silent failure.

## The four states of an async view

Model them explicitly with one value instead of several booleans:

```tsx
type View<T> =
  | { status: 'loading' }
  | { status: 'error'; message: string }
  | { status: 'empty' }
  | { status: 'ready'; data: T }
```

- **Loading**: show text such as "Loading invoices..." or a simple placeholder that keeps the layout from jumping. Put `aria-busy="true"` on the region. On a refresh, keep showing the old data and mark it as updating.
- **Error**: say what failed and offer a "Try again" button that repeats the request. Do not show raw server text for unexpected failures, and never leave the area blank.
- **Empty**: explain what belongs here and give the next action ("No customers yet. Add the first one."). An empty list can also mean the user's access policy hides rows, so keep the wording neutral ("No customers to show").
- **Ready**: the data. Handle a single row and very long text without breaking the layout.

## Forms

- One `<form onSubmit>` with `type="submit"` on the primary button. Call `event.preventDefault()`. Enter must submit.
- Every field has a visible label. Use the right `type` (`email`, `number`, `date`, `tel`), `autoComplete`, and `required` where it applies.
- Keep field values as strings in state and convert when you build the request. Keep what the user typed after a failure: never clear the form on error.
- Validate on submit, and re-validate a field when it loses focus after it was marked invalid. Show the message next to the field, linked with `aria-describedby`, and focus the first invalid field. Check the same rules the schema enforces (required columns, enum values, number ranges) and say what is wrong in plain words.
- While a request is running, disable the submit button and show "Saving..." so a double click cannot send it twice. Re-enable it in `finally`.
- After success, say so with the same verb as the button ("Customer saved"), then update the view: close the form, go back to the list, or reset it. A confirmation must not live only in a toast that disappears.

```tsx
async function handleSubmit(event: FormEvent<HTMLFormElement>) {
  event.preventDefault()
  const problems = validate(values)
  setErrors(problems)
  if (Object.keys(problems).length > 0) return
  setSaving(true)
  setFormError(null)
  try {
    await getOrbit().table('customers').create(toBody(values))
    onSaved()
  } catch (err) {
    setFormError(describeSaveError(err))
  } finally {
    setSaving(false)
  }
}
```

## Server errors

The client throws an `Error` with the server's English message. Map the cases you can recognize to your own text, and keep a generic fallback:

- A message that says a column is required, or a value is not allowed or invalid: point at the field if the message names a column, otherwise say "Check the highlighted fields".
- A message that says the row already exists (unique constraint): "That value is already in use."
- A message about a referenced row: "This item is still used elsewhere, so it cannot be removed."
- A message about a row-level security policy: "You do not have permission to do this."
- Anything else: "Something went wrong. Try again." Never expose internal details.

## Destructive actions

- Confirm before deleting or overwriting. Use a small dialog or an inline "Delete? Yes / No" step that names the item. The destructive button says exactly what it does ("Delete customer"), and the safe option is the default focus.
- After a successful delete, remove the item from the view and say it was deleted. If the delete fails, keep the item and explain why.
- Do not use optimistic updates for destructive actions. For harmless ones (toggling a flag) update the screen first and restore it if the request fails.

## Checklist before you finish

- Every request has loading, error and empty handling, and a visible way to retry.
- A form cannot be submitted twice, keeps the user's input on failure, and shows errors by the field.
- No error is swallowed in an empty `catch`.
- Text is in the user's language; identifiers and comments are in English.
- `run_build` passes for `typecheck`, `lint` and `build`.
