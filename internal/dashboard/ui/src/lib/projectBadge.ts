/**
 * Badge of a project in the collapsed sidebar (home-chat HCH-13): the first
 * letter of the title on a tinted square, so the icons are told apart. The tint
 * comes from the id, so a project keeps its color across sessions and renames.
 */

/** Theme tokens only (no literal colors); each pair is readable in both themes. */
export const BADGE_TINTS = [
  'bg-[var(--primary-tint)]',
  'bg-[var(--accent-tint)]',
  'bg-[var(--success-tint)]',
  'bg-[var(--warning-tint)]',
] as const

/** First letter of the title, upper-cased, by code point. '' for a blank title. */
export function projectInitial(title: string): string {
  const first = Array.from(title.trim())[0]
  return first ? first.toLocaleUpperCase() : ''
}

/** Stable index into BADGE_TINTS for an id (small string hash, not a security use). */
export function badgeTint(id: string): (typeof BADGE_TINTS)[number] {
  let h = 0
  for (const ch of id) h = (h * 31 + ch.codePointAt(0)!) >>> 0
  return BADGE_TINTS[h % BADGE_TINTS.length]
}
