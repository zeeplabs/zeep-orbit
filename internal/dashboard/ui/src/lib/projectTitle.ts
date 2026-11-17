/** Longest project title derived from a prompt (home-chat spec, assumption 3). */
export const PROJECT_TITLE_MAX = 60

/**
 * The title of a new project: the first 60 characters of the prompt, cut at a
 * word boundary, with whitespace collapsed. Counts code points (not UTF-16
 * units) so an accent or an emoji is never split. A first word longer than the
 * limit is cut hard. Returns "" for a blank prompt.
 */
export function projectTitleFromPrompt(prompt: string, max = PROJECT_TITLE_MAX): string {
  const text = prompt.replace(/\s+/g, ' ').trim()
  const chars = Array.from(text)
  if (chars.length <= max) return text

  const head = chars.slice(0, max)
  // The cut already falls on a word boundary.
  if (chars[max] === ' ') return head.join('').trimEnd()

  const lastSpace = head.lastIndexOf(' ')
  if (lastSpace > 0) return head.slice(0, lastSpace).join('').trimEnd()
  return head.join('')
}
