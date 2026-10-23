import { test, expect, Route } from '@playwright/test'
import { login } from './helpers'

/**
 * Keyboard / screen-reader behavior of shared dashboard pieces: DataTable, the skip
 * link, the page title, the tablet icon rail and the mobile "More" sheet. Data is
 * mocked where a table needs rows; the shell, nav and Settings run against the real
 * backend.
 */

const json = (route: Route, body: unknown) =>
  route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(body) })

test.describe('DataTable', () => {
  test('rows expand from the keyboard, the chevron toggles once, and the table is named', async ({ page }) => {
    await page.route(/\/dashboard\/api\/logs\/metrics/, (r) =>
      json(r, { total_requests: 2, requests_per_app: {}, avg_latency_ms: 1, errors_4xx: 0, errors_5xx: 0, method_breakdown: {} }),
    )
    await page.route(/\/dashboard\/api\/logs\?/, (r) =>
      json(r, [
        { timestamp: '2026-10-20T10:00:00Z', app: 'd', method: 'GET', path: '/a', query: 'marker=one', status: 200, latency_ms: 1 },
        { timestamp: '2026-10-20T10:01:00Z', app: 'd', method: 'POST', path: '/b', status: 404, latency_ms: 2 },
      ]),
    )
    await page.goto('/dashboard/logs')
    await expect(page.locator('tbody tr').first()).toBeVisible()

    // table has an accessible name and no empty column header
    await expect(page.getByRole('table', { name: 'Request logs' })).toBeVisible()
    const emptyHeaders = await page
      .locator('thead th')
      .evaluateAll((ths) => ths.filter((th) => !th.textContent?.trim()).length)
    expect(emptyHeaders).toBe(0)

    const expand = page.getByRole('button', { name: 'Expand row' })
    await expect(expand).toHaveCount(2)
    await expand.first().focus()
    await page.keyboard.press('Enter')
    await expect(page.getByRole('button', { name: 'Collapse row' })).toHaveAttribute('aria-expanded', 'true')
    await expect(page.getByText('marker=one')).toBeVisible()

    await page.keyboard.press('Space')
    await expect(page.getByText('marker=one')).toHaveCount(0)

    // mouse on the chevron: exactly one toggle (the row click must not fire twice)
    await page.getByRole('button', { name: 'Expand row' }).first().click()
    await expect(page.getByRole('button', { name: 'Collapse row' })).toHaveCount(1)
  })

  test('sortable headers are buttons with aria-sort that sort from the keyboard', async ({ page }) => {
    await page.route('**/dashboard/api/data-browser/apps', (r) =>
      json(r, [{ name: 'shop', tables: [{ name: 'orders', count: 1, columns: [{ name: 'id', type: 'uuid' }, { name: 'name', type: 'text' }] }] }]),
    )
    await page.route('**/dashboard/api/data-browser/query?*', (r) =>
      json(r, { data: [{ id: 'a', name: 'x' }], count: 1, limit: 50, offset: 0 }),
    )
    await page.goto('/dashboard/data-browser?app=shop&table=orders')
    await expect(page.locator('tbody tr').first()).toBeVisible()

    const nameTh = page.locator('th[aria-sort]').filter({ hasText: /name/i })
    await expect(nameTh).toHaveAttribute('aria-sort', 'none')
    const button = nameTh.getByRole('button')
    await button.focus()
    await page.keyboard.press('Enter')
    await expect(page).toHaveURL(/sort=name\.asc/)
    await expect(nameTh).toHaveAttribute('aria-sort', 'ascending')
    await page.keyboard.press('Space')
    await expect(page).toHaveURL(/sort=name\.desc/)
    await expect(nameTh).toHaveAttribute('aria-sort', 'descending')
  })
})

test.describe('Shell', () => {
  test('skip link is the first tab stop and moves focus to the main landmark', async ({ page }) => {
    await page.goto('/dashboard/apps')
    await expect(page.locator('main#main-content')).toBeVisible()

    await page.keyboard.press('Tab')
    const skip = page.getByRole('link', { name: 'Skip to main content' })
    await expect(skip).toBeFocused()
    await expect(skip).toBeInViewport()

    await page.keyboard.press('Enter')
    await expect(page.locator('main#main-content')).toBeFocused()
    await expect(page).toHaveURL(/\/dashboard\/apps$/)
  })

  test('document title follows the current section', async ({ page }) => {
    await page.goto('/dashboard/logs')
    await expect(page).toHaveTitle(/^Logs · /)
    await page.goto('/dashboard/data-browser')
    await expect(page).toHaveTitle(/^Data Browser · /)
  })

  test('<html lang> follows the selected language', async ({ page }) => {
    // The shell re-applies the logged-in user's saved language, so switch it through the
    // real footer toggle (which persists it) and put it back afterwards.
    await page.goto('/dashboard/apps')
    await expect(page.locator('html')).toHaveAttribute('lang', 'en')
    const aside = page.locator('aside')
    try {
      await aside.locator('button', { hasText: '🇺🇸' }).click()
      await expect(page.locator('html')).toHaveAttribute('lang', 'pt-BR')
    } finally {
      await aside.locator('button', { hasText: '🇧🇷' }).click()
      await expect(page.locator('html')).toHaveAttribute('lang', 'en')
    }
  })
})

test.describe('Tablet icon rail', () => {
  test.use({ viewport: { width: 820, height: 1180 } })

  test('nav links keep an accessible name when the labels are hidden', async ({ page }) => {
    await page.goto('/dashboard/apps')
    const aside = page.locator('aside')
    await expect(aside).toBeVisible()
    for (const name of ['Apps', 'Data Browser', 'Logs']) {
      await expect(aside.getByRole('link', { name })).toBeVisible()
    }
  })
})

test.describe('Mobile "More" sheet', () => {
  test.use({ viewport: { width: 390, height: 844 } })

  test('is a modal dialog: focus trapped, Esc closes, focus returns to the trigger', async ({ page }) => {
    await page.goto('/dashboard/apps')
    const bar = page.locator('nav.fixed.inset-x-0.bottom-0')
    const more = bar.getByRole('button', { name: 'More' })
    // While the modal is open the rest of the page is aria-hidden, so the trigger can't be
    // found by role then; address it structurally for the open-state assertions.
    const trigger = bar.locator('button[aria-haspopup="dialog"]')
    await expect(more).toHaveAttribute('aria-expanded', 'false')

    await more.click()
    const dialog = page.getByRole('dialog', { name: 'More' })
    await expect(dialog).toBeVisible()
    await expect(trigger).toHaveAttribute('aria-expanded', 'true')
    expect(await page.evaluate(() => !!document.activeElement?.closest('[role="dialog"]'))).toBe(true)

    for (let i = 0; i < 30; i++) {
      await page.keyboard.press('Tab')
      expect(await page.evaluate(() => !!document.activeElement?.closest('[role="dialog"]'))).toBe(true)
    }

    await page.keyboard.press('Escape')
    await expect(dialog).toHaveCount(0)
    await expect(more).toBeFocused()
  })

  test('closes by itself when the viewport grows past the mobile breakpoint', async ({ page }) => {
    await page.goto('/dashboard/apps')
    await page.locator('nav.fixed.inset-x-0.bottom-0').getByRole('button', { name: 'More' }).click()
    await expect(page.getByRole('dialog', { name: 'More' })).toBeVisible()

    await page.setViewportSize({ width: 1024, height: 800 })
    await expect(page.getByRole('dialog')).toHaveCount(0)
    // Radix releases the body lock a tick after unmounting, so poll instead of reading once.
    await expect
      .poll(() => page.evaluate(() => getComputedStyle(document.body).pointerEvents))
      .not.toBe('none')
  })

  test('Log out from the sheet opens its confirmation only after the sheet closes', async ({ page }) => {
    await page.goto('/dashboard/apps')
    await page.locator('nav.fixed.inset-x-0.bottom-0').getByRole('button', { name: 'More' }).click()
    await page.getByRole('dialog', { name: 'More' }).getByRole('button', { name: 'Log out' }).click()

    await expect(page.getByRole('dialog', { name: 'More' })).toHaveCount(0)
    const confirm = page.getByRole('dialog')
    await expect(confirm).toHaveCount(1)
    expect(await page.evaluate(() => !!document.activeElement?.closest('[role="dialog"]'))).toBe(true)
    await page.keyboard.press('Escape')
    await expect(confirm).toHaveCount(0)
  })
})

test.describe('Unsaved changes guard', () => {
  async function beforeUnloadPrevented(page: import('@playwright/test').Page) {
    return page.evaluate(() => {
      const e = new Event('beforeunload', { cancelable: true })
      window.dispatchEvent(e)
      return e.defaultPrevented
    })
  }

  test('Branding form asks before leaving only while the field differs from the saved value', async ({ page }) => {
    await page.goto('/dashboard/configuracoes')
    const input = page.locator('input[placeholder="Zeep Tecnologia"]')
    await expect(input).toBeVisible()
    await expect.poll(() => beforeUnloadPrevented(page)).toBe(false)

    const original = await input.inputValue()
    await input.fill(`${original} edited`)
    expect(await beforeUnloadPrevented(page)).toBe(true)

    await input.fill(original)
    expect(await beforeUnloadPrevented(page)).toBe(false)
  })
  test('in-app navigation asks first: stay keeps the edit, leave navigates, a clean form never asks', async ({ page }) => {
    await page.goto('/dashboard/configuracoes')
    const input = page.locator('input[placeholder="Zeep Tecnologia"]')
    await expect(input).toBeVisible()
    const nav = page.locator('aside').getByRole('link', { name: 'Apps' })
    const dialog = page.getByRole('dialog', { name: 'Leave without saving?' })

    // clean form: no prompt
    await nav.click()
    await expect(page).toHaveURL(/\/dashboard\/apps$/)
    await expect(dialog).toHaveCount(0)

    // dirty form: link click is blocked and the edit survives "Stay". Reach the page
    // through the SPA (not page.goto) so "back" below is a same-document popstate.
    await page.locator('aside').getByRole('link', { name: 'Settings' }).click()
    await expect(input).toBeVisible()
    const original = await input.inputValue()
    await input.fill(`${original} edited`)
    await nav.click()
    await expect(dialog).toBeVisible()
    await dialog.getByRole('button', { name: 'Stay' }).click()
    await expect(dialog).toHaveCount(0)
    await expect(page).toHaveURL(/\/configuracoes$/)
    await expect(input).toHaveValue(`${original} edited`)

    // browser back is blocked too; "Leave page" goes through
    await page.goBack()
    await expect(dialog).toBeVisible()
    await dialog.getByRole('button', { name: 'Leave page' }).click()
    await expect(page).not.toHaveURL(/\/configuracoes$/)
    await expect(dialog).toHaveCount(0)
  })

  test('switching the Settings tab with a dirty form asks first; stay keeps the edit, leave switches', async ({ page }) => {
    await page.goto('/dashboard/configuracoes')
    const input = page.locator('input[placeholder="Zeep Tecnologia"]')
    await expect(input).toBeVisible()
    const dialog = page.getByRole('dialog', { name: 'Leave without saving?' })
    const databaseTab = page.getByRole('tab', { name: 'Database' })

    // clean form: the tab switches without a prompt, then back
    await databaseTab.click()
    await expect(page).toHaveURL(/tab=database/)
    await expect(dialog).toHaveCount(0)
    await page.getByRole('tab', { name: 'Branding' }).click()
    await expect(input).toBeVisible()

    // dirty form: unmounting it would lose the edit, so the switch is held back
    const original = await input.inputValue()
    await input.fill(`${original} edited`)
    await databaseTab.click()
    await expect(dialog).toBeVisible()
    await dialog.getByRole('button', { name: 'Stay' }).click()
    await expect(dialog).toHaveCount(0)
    await expect(page).not.toHaveURL(/tab=database/)
    await expect(input).toHaveValue(`${original} edited`)

    await databaseTab.click()
    await dialog.getByRole('button', { name: 'Leave page' }).click()
    await expect(page).toHaveURL(/tab=database/)
    await expect(dialog).toHaveCount(0)
  })

  test('App Details Login tab is clean after a save even when the server normalizes the values', async ({ page }) => {
    // Saving Google sign-in with an empty redirect URL stores the default one, and the
    // domains come back as "a.com, b.com": the form must compare with the server again
    // after the refetch instead of staying "dirty" against what was typed.
    const res = await page.request.post('/dashboard/api/apps', { data: { name: `guard_${Date.now()}` } })
    expect(res.ok(), await res.text()).toBeTruthy()
    const app = await res.json()

    await page.goto(`/dashboard/apps/${app.id}`)
    await page.getByRole('tab', { name: 'Login' }).click()
    // The Google card starts collapsed while the provider is off; once open, the switches
    // are, in order, Email and Google.
    await page.getByRole('button', { name: /^Google Sign in with Google/ }).click()
    await page.getByRole('switch').nth(1).click()
    await page.getByPlaceholder('Google OAuth Client ID').fill('client-id.apps.googleusercontent.com')
    const secret = page.getByPlaceholder('Client Secret')
    await secret.fill('client-secret')
    await page.getByPlaceholder('zeeplabs.com, zeepfy.com').fill('a.com,b.com')
    expect(await beforeUnloadPrevented(page)).toBe(true)

    await page.getByRole('button', { name: 'Save', exact: true }).click()
    await expect(page.getByText('Saved.')).toBeVisible()
    await expect(page.getByPlaceholder('zeeplabs.com, zeepfy.com')).toHaveValue('a.com, b.com')
    // The secret is never returned by the server; the field is cleared as after a reload.
    await expect(secret).toHaveValue('')
    await expect.poll(() => beforeUnloadPrevented(page)).toBe(false)

    await page.getByRole('tab', { name: 'API' }).click()
    await expect(page.getByRole('dialog', { name: 'Leave without saving?' })).toHaveCount(0)
    await expect(page.getByRole('tab', { name: 'API' })).toHaveAttribute('aria-selected', 'true')

    await page.request.delete(`/dashboard/api/apps/${app.id}`)
  })
})

test.describe('Unsaved changes guard on logout', () => {
  // Logout revokes the session server-side, which would kill the shared storageState the
  // other specs reuse, so this one signs in on its own.
  test.use({ storageState: { cookies: [], origins: [] } })

  test('logging out with a dirty form warns in the logout dialog and skips the native prompt', async ({ page }) => {
    await login(page)
    await page.goto('/dashboard/configuracoes')
    const input = page.locator('input[placeholder="Zeep Tecnologia"]')
    await expect(input).toBeVisible()
    await input.fill(`${await input.inputValue()} edited`)

    // The browser prompt would come after the session is already revoked, and "Stay" would
    // leave a page whose saves can only fail. The logout dialog warns instead.
    const nativePrompts: string[] = []
    page.on('dialog', (d) => {
      nativePrompts.push(d.type())
      void d.accept()
    })
    await page.getByRole('button', { name: 'Log Out' }).first().click()
    const confirm = page.getByRole('dialog')
    await expect(confirm.getByText('You have unsaved changes on this page.', { exact: false })).toBeVisible()
    await confirm.getByRole('button', { name: 'Log Out' }).click()
    await page.waitForURL('**/login')
    expect(nativePrompts).toEqual([])
    await expect(page.getByRole('dialog', { name: 'Leave without saving?' })).toHaveCount(0)
  })
})
