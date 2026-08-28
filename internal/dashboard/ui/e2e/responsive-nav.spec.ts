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
    // Default language is English (see src/lib/i18n.ts) unless localStorage
    // sets otherwise -- assert the real rendered text, not a translation
    // that wouldn't exist yet (a trivially-passing "hidden" check on absent
    // text proves nothing).
    await expect(aside.getByText('General', { exact: true })).toBeHidden()

    // A thin separator marks the section boundary in place of the hidden title.
    await expect(aside.getByRole('separator')).toHaveCount(2) // between 3 sections

    // Mobile bottom bar ("More" button) must not be visible at tablet width.
    await expect(page.getByRole('button', { name: 'More' })).toBeHidden()

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

// RESP-01: mobile (390x844, `mobile` project) gets a 5-slot bottom bar
// (Apps/Data Browser/Logs/SDKs + "More") instead of the sidebar/rail.
test.describe('Mobile 5-slot bottom bar', () => {
  test('renders 5 fixed-bar slots and the "More" drawer lists the rest, role-filtered', async ({
    page,
  }, testInfo) => {
    test.skip(testInfo.project.name !== 'mobile', 'mobile-only assertions')
    await bootstrapOrSkip(page)
    await login(page)
    await page.goto('/dashboard/apps')

    // The fixed bottom bar is the only <nav> with these positioning classes;
    // the "More" sheet's inner <nav data-testid="mobile-nav-sheet"> doesn't
    // share them, so this selector stays unique even once the sheet is open.
    const bottomBar = page.locator('nav.fixed.inset-x-0.bottom-0')
    await expect(bottomBar).toBeVisible()
    await expect(bottomBar.getByRole('link', { name: 'Apps' })).toBeVisible()
    await expect(bottomBar.getByRole('link', { name: 'Data Browser' })).toBeVisible()
    await expect(bottomBar.getByRole('link', { name: 'Logs' })).toBeVisible()
    await expect(bottomBar.getByRole('link', { name: 'SDKs' })).toBeVisible()
    await expect(bottomBar.getByRole('button', { name: 'More' })).toBeVisible()

    // Tapping a fixed slot navigates and marks it active.
    await bottomBar.getByRole('link', { name: 'Logs' }).click()
    await expect(page).toHaveURL(/\/dashboard\/logs$/)
    await expect(bottomBar.getByRole('link', { name: 'Logs' })).toHaveAttribute(
      'aria-current',
      'page',
    )

    // "More" opens the sheet listing the role-filtered remainder
    // (superadmin test user sees Users/Audit/Integrations/Settings/MCP).
    await bottomBar.getByRole('button', { name: 'More' }).click()
    const sheet = page.getByTestId('mobile-nav-sheet')
    await expect(sheet).toBeVisible()
    await expect(sheet.getByRole('link', { name: 'MCP' })).toBeVisible()
    await expect(sheet.getByRole('link', { name: 'Users' })).toBeVisible()
    await expect(sheet.getByRole('link', { name: 'Audit' })).toBeVisible()
    await expect(sheet.getByRole('link', { name: 'Integrations' })).toBeVisible()
    await expect(sheet.getByRole('link', { name: 'Settings' })).toBeVisible()

    // Bottom bar stays above the safe-area inset (existing padding, no
    // regression): its own height (60px) plus inset must fit the viewport.
    const box = await bottomBar.boundingBox()
    expect(box?.height).toBeGreaterThanOrEqual(60)
  })
})

// RESP-03: ultra-wide (2560x1440, `ultrawide` project) caps the main content
// area at 1920px instead of stretching it edge-to-edge.
test.describe('Ultra-wide content cap', () => {
  test('caps content at 1920px and produces no page-level horizontal overflow', async ({
    page,
  }, testInfo) => {
    test.skip(testInfo.project.name !== 'ultrawide', 'ultrawide-only assertions')
    await bootstrapOrSkip(page)
    await login(page)
    await page.goto('/dashboard/apps')

    // DashboardShell.tsx renders exactly one direct <div> child of <main> --
    // the capped/centered content wrapper around <Outlet/>.
    const content = page.locator('main > div').first()
    const box = await content.boundingBox()
    expect(box?.width).toBeLessThanOrEqual(1920)

    const overflowX = await page.evaluate(
      () => document.documentElement.scrollWidth - window.innerWidth,
    )
    expect(overflowX).toBeLessThanOrEqual(0)

    await page.goto('/dashboard/data-browser')
    const overflowXDataBrowser = await page.evaluate(
      () => document.documentElement.scrollWidth - window.innerWidth,
    )
    expect(overflowXDataBrowser).toBeLessThanOrEqual(0)
  })
})

// RESP-05: min-w-[420px] on several BrandSettingsPage.tsx/GitHubIntegrationPage.tsx
// fields forced horizontal overflow below 420px. Both pages must render
// clean at 375px (iPhone SE class) now.
test.describe('No overflow below 420px on settings pages', () => {
  test('brand settings and GitHub integration pages produce no horizontal overflow at 375px', async ({
    page,
  }, testInfo) => {
    test.skip(testInfo.project.name !== 'mobile', 'mobile-only assertions')
    await bootstrapOrSkip(page)
    await login(page)

    await page.goto('/dashboard/configuracoes')
    const brandOverflow = await page.evaluate(
      () => document.documentElement.scrollWidth - window.innerWidth,
    )
    expect(brandOverflow).toBeLessThanOrEqual(0)

    await page.goto('/dashboard/integracoes/github')
    const githubOverflow = await page.evaluate(
      () => document.documentElement.scrollWidth - window.innerWidth,
    )
    expect(githubOverflow).toBeLessThanOrEqual(0)
  })
})
