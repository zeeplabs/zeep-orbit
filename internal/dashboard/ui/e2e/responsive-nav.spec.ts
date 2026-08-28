import { test, expect } from '@playwright/test'
import { bootstrapOrSkip, login } from './helpers'

// RESP-02: tablet (820x1180, `tablet` project) gets a 72px icon-only rail —
// not the full 264px sidebar, not the mobile bottom bar. This file is shared
// by the mobile/tablet/ultrawide projects (see playwright.config.ts), so each
// test guards itself to the one project its assertions are valid for.
test.describe('Tablet icon-only sidebar rail', () => {
  test('renders a 72px rail with tooltip and hides the full sidebar/bottom bar', async ({
    page,
  }, testInfo) => {
    test.skip(testInfo.project.name !== 'tablet', 'tablet-only assertions')
    await bootstrapOrSkip(page)
    await login(page)
    await page.goto('/dashboard/apps')

    const aside = page.locator('aside')
    await expect(aside).toBeVisible()
    const box = await aside.boundingBox()
    expect(box?.width).toBeGreaterThanOrEqual(64)
    expect(box?.width).toBeLessThanOrEqual(80)

    // Wordmark (only shown in the full 264px sidebar) must not be visible
    // at rail width.
    await expect(aside.locator('img[alt="Orbit"]')).toBeHidden()

    // Section titles (only shown at desktop width) are hidden in rail mode.
    await expect(aside.getByText('Geral', { exact: true })).toBeHidden()

    // A thin separator marks the section boundary in place of the hidden title.
    await expect(aside.getByRole('separator')).toHaveCount(2) // between 3 sections

    // Mobile bottom bar ("More" button) must not be visible at tablet width.
    await expect(page.getByRole('button', { name: 'Mais' })).toBeHidden()

    // Hovering the Apps rail icon shows its tooltip label.
    const appsLink = page.locator('aside a[href="/dashboard/apps"]')
    await appsLink.hover()
    await expect(page.getByRole('tooltip', { name: 'Apps' })).toBeVisible()

    // Clicking a rail icon still navigates correctly and applies the active
    // state (React Router's aria-current="page" on the matched NavLink).
    const dataBrowserLink = page.locator('aside a[href="/dashboard/data-browser"]')
    await dataBrowserLink.click()
    await expect(page).toHaveURL(/\/dashboard\/data-browser$/)
    await expect(page.locator('aside a[href="/dashboard/data-browser"]')).toHaveAttribute(
      'aria-current',
      'page',
    )
  })
})
