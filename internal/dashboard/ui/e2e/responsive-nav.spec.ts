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

    // Nav-item labels themselves (not just section titles) are hidden at
    // rail width -- reverting NavRow's `hidden lg:inline` on the label span
    // must fail this.
    await expect(aside.getByText('Apps', { exact: true })).toBeHidden()

    // Every role-visible NAV_SECTIONS item is present in the rail (all 9
    // items for the superadmin test user), not just a subset.
    for (const href of [
      '/dashboard/apps',
      '/dashboard/data-browser',
      '/dashboard/logs',
      '/dashboard/sdks',
      '/dashboard/mcp-settings',
      '/dashboard/usuarios',
      '/dashboard/auditoria',
      '/dashboard/integracoes/github',
      '/dashboard/configuracoes',
    ]) {
      await expect(aside.locator(`a[href="${href}"]`)).toBeVisible()
    }

    // A thin separator marks the section boundary in place of the hidden title.
    await expect(aside.getByRole('separator')).toHaveCount(2) // between 3 sections
    await expect(aside.getByRole('separator').first()).toBeVisible()

    // Mobile bottom bar ("More" button) must not be visible at tablet width.
    await expect(page.getByRole('button', { name: 'More' })).toBeHidden()

    // Hovering the Apps rail icon shows its tooltip label.
    const appsLink = page.locator('aside a[href="/dashboard/apps"]')
    await appsLink.hover()
    await expect(page.getByRole('tooltip', { name: 'Apps' })).toBeVisible()

    // Focusing (not just hovering) the icon shows the same tooltip -- Radix
    // Tooltip.Trigger fires on either interaction path. Reload first so the
    // hover above doesn't leave the tooltip already open/animating.
    await page.reload()
    await appsLink.focus()
    await expect(page.getByRole('tooltip', { name: 'Apps' })).toBeVisible()
    await appsLink.blur()

    // Clicking a rail icon still navigates correctly and applies the active
    // state (React Router's aria-current="page" on the matched NavLink) with
    // the desktop-equivalent fill/weight/tint, not just the ARIA attribute.
    const dataBrowserLink = page.locator('aside a[href="/dashboard/data-browser"]')
    await dataBrowserLink.click()
    await expect(page).toHaveURL(/\/dashboard\/data-browser$/)
    await expect(page.locator('aside a[href="/dashboard/data-browser"]')).toHaveAttribute(
      'aria-current',
      'page',
    )
    const activeWeight = Number(
      await dataBrowserLink.evaluate((el) => getComputedStyle(el).fontWeight),
    )
    const inactiveWeight = Number(await appsLink.evaluate((el) => getComputedStyle(el).fontWeight))
    expect(activeWeight).toBeGreaterThan(inactiveWeight)
    const activeColor = await dataBrowserLink.evaluate((el) => getComputedStyle(el).color)
    const inactiveColor = await appsLink.evaluate((el) => getComputedStyle(el).color)
    expect(activeColor).not.toBe(inactiveColor)
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
    // Exactly 5 slots -- not just "these 4 are present" (a 6th slot must fail
    // this).
    await expect(bottomBar.locator(':scope > *')).toHaveCount(5)
    await expect(bottomBar.getByRole('link', { name: 'Apps' })).toBeVisible()
    await expect(bottomBar.getByRole('link', { name: 'Data Browser' })).toBeVisible()
    await expect(bottomBar.getByRole('link', { name: 'Logs' })).toBeVisible()
    await expect(bottomBar.getByRole('link', { name: 'SDKs' })).toBeVisible()
    await expect(bottomBar.getByRole('button', { name: 'More' })).toBeVisible()

    // Tapping a fixed slot navigates and marks it active with the
    // desktop-equivalent fill/weight/tint, not just the ARIA attribute.
    await bottomBar.getByRole('link', { name: 'Logs' }).click()
    await expect(page).toHaveURL(/\/dashboard\/logs$/)
    const logsLink = bottomBar.getByRole('link', { name: 'Logs' })
    const appsLinkBar = bottomBar.getByRole('link', { name: 'Apps' })
    await expect(logsLink).toHaveAttribute('aria-current', 'page')
    const activeWeight = Number(await logsLink.evaluate((el) => getComputedStyle(el).fontWeight))
    const inactiveWeight = Number(
      await appsLinkBar.evaluate((el) => getComputedStyle(el).fontWeight),
    )
    expect(activeWeight).toBeGreaterThan(inactiveWeight)
    const activeColor = await logsLink.evaluate((el) => getComputedStyle(el).color)
    const inactiveColor = await appsLinkBar.evaluate((el) => getComputedStyle(el).color)
    expect(activeColor).not.toBe(inactiveColor)

    // "More" opens the sheet listing the role-filtered remainder
    // (superadmin test user sees Users/Audit/Integrations/Settings/MCP),
    // grouped under the same section headings and order as the desktop
    // sidebar (nav.ts's NAV_SECTIONS: General, Deployment, Superadmin).
    await bottomBar.getByRole('button', { name: 'More' }).click()
    const sheet = page.getByTestId('mobile-nav-sheet')
    await expect(sheet).toBeVisible()
    await expect(sheet.locator('span.uppercase')).toHaveText(['General', 'Deployment', 'Superadmin'])
    await expect(sheet.getByRole('link', { name: 'MCP' })).toBeVisible()
    await expect(sheet.getByRole('link', { name: 'Users' })).toBeVisible()
    await expect(sheet.getByRole('link', { name: 'Audit' })).toBeVisible()
    await expect(sheet.getByRole('link', { name: 'Integrations' })).toBeVisible()
    await expect(sheet.getByRole('link', { name: 'Settings' })).toBeVisible()

    // Bottom bar's bottom padding is wired to the safe-area inset -- assert
    // the mechanism itself (the unresolved `env()` expression React set on
    // the element), not just the bar's rendered height, which is a hardcoded
    // constant (MobileNav.tsx `height: 60`) that a getComputedStyle read
    // would report as 60 regardless of whether the inset rule exists (it
    // resolves to 0px under headless-Chrome emulation either way).
    const paddingBottom = await bottomBar.evaluate((el) => (el as HTMLElement).style.paddingBottom)
    expect(paddingBottom).toContain('env(safe-area-inset-bottom')
  })
})

test.describe('Mobile bottom bar role-gates the "More" drawer', () => {
  test('a role with no platformAction permissions sees only ungated items', async ({
    page,
  }, testInfo) => {
    test.skip(testInfo.project.name !== 'mobile', 'mobile-only assertions')
    await bootstrapOrSkip(page)
    await login(page)

    // Downgrade the logged-in session to 'member' for the client only --
    // 'member' has none of users/audit/integrations/branding (see
    // src/lib/permissions.ts PLATFORM_PERMISSIONS), so every gated item in
    // NAV_SECTIONS' Superadmin section must be absent from the drawer.
    await page.route('**/dashboard/api/me', async (route) => {
      const response = await route.fetch()
      const body = await response.json()
      await route.fulfill({ response, json: { ...body, role: 'member' } })
    })
    await page.goto('/dashboard/apps')

    await page.locator('nav.fixed.inset-x-0.bottom-0').getByRole('button', { name: 'More' }).click()
    const sheet = page.getByTestId('mobile-nav-sheet')
    await expect(sheet).toBeVisible()

    // Ungated item stays visible.
    await expect(sheet.getByRole('link', { name: 'MCP' })).toBeVisible()

    // Every gated item is omitted, not merely hidden -- toHaveCount(0)
    // rather than toBeHidden(), since the omission is a conditional render
    // (`visible.length === 0 return null` in MobileNav.tsx), not a CSS hide.
    await expect(sheet.getByRole('link', { name: 'Users' })).toHaveCount(0)
    await expect(sheet.getByRole('link', { name: 'Audit' })).toHaveCount(0)
    await expect(sheet.getByRole('link', { name: 'Integrations' })).toHaveCount(0)
    await expect(sheet.getByRole('link', { name: 'Settings' })).toHaveCount(0)
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
    // Wait for the route to actually paint before measuring -- immediately
    // after goto() the shell can still show its loading fallback, where
    // scrollWidth === innerWidth unconditionally and proves nothing.
    await expect(page.getByRole('heading', { name: 'Your apps' })).toBeVisible()

    // DashboardShell.tsx renders exactly one direct <div> child of <main> --
    // the capped/centered content wrapper around <Outlet/>.
    const content = page.locator('main > div').first()
    const box = await content.boundingBox()
    expect(box?.width).toBeLessThanOrEqual(1920)

    // Centered: equal space on both sides of the capped content within
    // <main>, and the sidebar stays pinned to the viewport's left edge
    // rather than being centered along with the content.
    const main = page.locator('main')
    const mainBox = await main.boundingBox()
    const leftGap = box!.x - mainBox!.x
    const rightGap = mainBox!.x + mainBox!.width - (box!.x + box!.width)
    expect(Math.abs(leftGap - rightGap)).toBeLessThanOrEqual(2)

    const asideBox = await page.locator('aside').boundingBox()
    expect(asideBox?.x).toBe(0)

    // No page-level horizontal overflow on any NAV_SECTIONS route, not just
    // Apps/Data Browser -- wait for the network to settle before measuring so
    // a route's loading fallback (scrollWidth === innerWidth unconditionally)
    // can't mask a real overflow.
    for (const route of [
      '/dashboard/apps',
      '/dashboard/data-browser',
      '/dashboard/logs',
      '/dashboard/sdks',
      '/dashboard/mcp-settings',
      '/dashboard/usuarios',
      '/dashboard/auditoria',
      '/dashboard/integracoes/github',
      '/dashboard/configuracoes',
    ]) {
      await page.goto(route)
      await page.waitForLoadState('networkidle')
      const overflowX = await page.evaluate(
        () => document.documentElement.scrollWidth - window.innerWidth,
      )
      expect(overflowX, `overflow on ${route}`).toBeLessThanOrEqual(0)
    }
  })
})

// RESP-03 AC3: at or below the 1920px cap, content keeps using full
// available width -- the cap only engages past it. `chromium` runs at
// Desktop Chrome's default viewport (well under 1920px), so this is the
// right project to prove the cap does NOT engage here.
test.describe('Content is full-width below the ultra-wide cap', () => {
  test('content wrapper fills the space right of the sidebar at a sub-cap desktop width', async ({
    page,
  }, testInfo) => {
    test.skip(testInfo.project.name !== 'chromium', 'chromium-only assertions')
    await bootstrapOrSkip(page)
    await login(page)
    await page.goto('/dashboard/apps')
    await expect(page.getByRole('heading', { name: 'Your apps' })).toBeVisible()

    const main = page.locator('main')
    const content = page.locator('main > div').first()
    const mainBox = await main.boundingBox()
    const box = await content.boundingBox()
    // Full width of <main> (minus its own centering, which is a no-op here
    // since content is narrower than <main> only once the cap engages) --
    // assert the two widths are equal, proving no cap is in effect.
    expect(Math.abs((mainBox?.width ?? 0) - (box?.width ?? 0))).toBeLessThanOrEqual(2)

    // RESP-03 AC1: the existing 264px full sidebar, unchanged at desktop
    // width -- no test asserted the literal width before.
    const asideBox = await page.locator('aside').boundingBox()
    expect(asideBox?.width).toBe(264)

    // RESP-02 AC4's counterpart: the section separator (visible at the
    // tablet rail width in place of the hidden section title) is hidden
    // again at desktop, where the section title itself is shown instead.
    await expect(page.locator('aside').getByRole('separator').first()).toBeHidden()
  })
})

// RESP-05: min-w-[420px] on several BrandSettingsPage.tsx/GitHubIntegrationPage.tsx
// fields forced horizontal overflow below 420px. Both pages must render
// clean at exactly 375px (iPhone SE class, the width the spec/Independent
// Test/Success Criteria all name) now.
test.describe('No overflow below 420px on settings pages', () => {
  test('brand settings and GitHub integration pages produce no horizontal overflow at 375px', async ({
    page,
  }, testInfo) => {
    test.skip(testInfo.project.name !== 'mobile', 'mobile-only assertions')
    await page.setViewportSize({ width: 375, height: 667 })
    await bootstrapOrSkip(page)
    await login(page)

    await page.goto('/dashboard/configuracoes')
    // Wait for real content before measuring -- immediately after goto() the
    // route can still show its loading fallback, where scrollWidth ===
    // innerWidth unconditionally and the min-w-[420px] bug (if reintroduced)
    // would go undetected.
    await expect(page.getByRole('heading', { name: 'Settings' })).toBeVisible()
    const brandOverflow = await page.evaluate(
      () => document.documentElement.scrollWidth - window.innerWidth,
    )
    expect(brandOverflow).toBeLessThanOrEqual(0)

    await page.goto('/dashboard/integracoes/github')
    await expect(page.getByRole('heading', { name: 'Integrations' })).toBeVisible()
    const githubOverflow = await page.evaluate(
      () => document.documentElement.scrollWidth - window.innerWidth,
    )
    expect(githubOverflow).toBeLessThanOrEqual(0)
  })
})

// RESP-07: DataBrowserPage's two-pane grid (240px_1fr) used to only collapse
// to a stacked mobile layout below 768px; it must now collapse below 1024px
// (tablet) too, matching the new nav breakpoint scheme.
test.describe('Data Browser tablet parity', () => {
  test('collapses the table-list panel at tablet width instead of the desktop grid', async ({
    page,
  }, testInfo) => {
    test.skip(testInfo.project.name !== 'tablet', 'tablet-only assertions')
    await bootstrapOrSkip(page)
    await login(page)
    await page.goto('/dashboard/data-browser')

    const display = await page
      .locator('div.grid.h-full.min-h-full.items-stretch')
      .evaluate((el) => getComputedStyle(el).display)
    expect(display).toBe('flex')

    // The paired panel class (max-lg:max-h-[220px] max-lg:overflow-y-auto)
    // caps the table-list panel's height once it's no longer a grid column --
    // asserted separately from the parent's `display` swap above.
    const panel = page
      .locator('div.grid.h-full.min-h-full.items-stretch > div')
      .first()
    const panelStyle = await panel.evaluate((el) => {
      const s = getComputedStyle(el)
      return { maxHeight: s.maxHeight, overflowY: s.overflowY }
    })
    expect(panelStyle.maxHeight).toBe('220px')
    expect(panelStyle.overflowY).toBe('auto')
  })
})

