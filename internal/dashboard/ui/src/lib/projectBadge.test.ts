import { describe, test, expect } from 'vitest'
import { BADGE_TINTS, badgeTint, projectInitial } from './projectBadge'

describe('projectInitial', () => {
  test('first letter, upper-cased', () => {
    expect(projectInitial('clinic app')).toBe('C')
    expect(projectInitial('  nutrition tracker')).toBe('N')
  })

  test('code-point aware: an accent or an emoji is not cut in half', () => {
    expect(projectInitial('água')).toBe('Á')
    expect(projectInitial('🚀 rocket')).toBe('🚀')
  })

  test('blank title gives an empty string', () => {
    expect(projectInitial('')).toBe('')
    expect(projectInitial('   ')).toBe('')
  })
})

describe('badgeTint', () => {
  test('same id, same tint', () => {
    expect(badgeTint('abc')).toBe(badgeTint('abc'))
  })

  test('always one of the tokens, and ids spread over more than one', () => {
    const tints = new Set(Array.from({ length: 40 }, (_, i) => badgeTint(`session-${i}`)))
    for (const t of tints) expect(BADGE_TINTS).toContain(t)
    expect(tints.size).toBeGreaterThan(1)
  })
})
