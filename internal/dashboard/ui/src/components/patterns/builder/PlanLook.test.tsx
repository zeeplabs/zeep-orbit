import { describe, test, expect } from 'vitest'
import { render, screen, within } from '@testing-library/react'
import { parsePlan } from './builderModel'
import { PlanBody } from './PlanCard'

const base = {
  summary: 'A catalogue for a small bookshop.',
  screens: [{ name: 'Catalogue', purpose: 'Browse books' }],
  assumptions: [],
  milestones: [{ title: 'Sign in and list', deliverable: 'See the list' }],
}

describe('plan look: visual direction', () => {
  test('parsePlan keeps direction, type, density and motion', () => {
    const plan = parsePlan({
      ...base,
      look: {
        direction: 'Warm and practical: cream tones, green accent',
        type: 'System serif headings, sans body',
        density: 'balanced',
        motion: 'Light fade on page entry',
        colors: 'Cream and green',
      },
    })
    expect(plan?.look).toMatchObject({
      direction: 'Warm and practical: cream tones, green accent',
      type: 'System serif headings, sans body',
      density: 'balanced',
      motion: 'Light fade on page entry',
      colors: 'Cream and green',
    })
  })

  test('a plan approved before the direction existed still parses, with the new fields absent', () => {
    const plan = parsePlan({ ...base, look: { style: 'clean', colors: 'teal' } })
    expect(plan).not.toBeNull()
    expect(plan?.look.direction).toBeUndefined()
    expect(plan?.look.motion).toBeUndefined()
    expect(plan?.look.style).toBe('clean')
  })

  test('PlanBody shows the direction first, then the other look rows, as plain text', () => {
    const plan = parsePlan({
      ...base,
      look: {
        direction: 'Calm and editorial <b>not bold</b>',
        type: 'Serif headings',
        density: 'airy',
        motion: 'Fade on entry',
        style: 'Clean',
        ui_language: 'pt-BR',
      },
    })!
    const { container } = render(<PlanBody plan={plan} />)

    const labels = Array.from(container.querySelectorAll('dt')).map((dt) => dt.textContent)
    expect(labels).toEqual(['Direction:', 'Typography:', 'Density:', 'Motion:', 'Style:', 'Interface language:'])

    const direction = container.querySelector('dt')!.nextElementSibling as HTMLElement
    expect(direction.textContent).toBe('Calm and editorial <b>not bold</b>')
    expect(direction.querySelector('b')).toBeNull() // the model's text is never rendered as HTML
    expect(within(container).getByText('Fade on entry')).toBeInTheDocument()
  })

  test('PlanBody for an old plan shows only the rows it has', () => {
    const plan = parsePlan({ ...base, look: { style: 'Clean' } })!
    render(<PlanBody plan={plan} />)
    expect(screen.queryByText('Direction:')).toBeNull()
    expect(screen.getByText('Style:')).toBeInTheDocument()
  })
})
