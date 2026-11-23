import { describe, expect, test } from 'vitest'
import { parsePlan } from './builderModel'

const base = {
  summary: 'A catalogue for a small bookshop.',
  screens: [{ name: 'Catalogue', purpose: 'Browse books' }],
  look: {},
  assumptions: [],
  milestones: [{ title: 'Sign in and list', deliverable: 'See the list' }],
}

describe('plan title', () => {
  test('parsePlan keeps the proposed project title', () => {
    expect(parsePlan({ ...base, title: 'Bookshop catalogue' })?.title).toBe('Bookshop catalogue')
  })

  test('a plan from before titles existed has an empty title, not a crash', () => {
    expect(parsePlan(base)?.title).toBe('')
  })
})
