import { test, expect } from '@playwright/test'

/**
 * A 401 from the API means the session is gone. The dashboard must send the user to the
 * login screen with a return_to pointing at where they were, not leave them on a page
 * full of generic errors. The 401 is mocked; the shell is real.
 */

test('a 401 from the API redirects to login with return_to', async ({ page }) => {
  await page.goto('/dashboard/apps')
  await expect(page.getByRole('heading', { name: 'Your apps' })).toBeVisible()

  await page.route(/\/dashboard\/api\/apps$/, (route) =>
    route.fulfill({ status: 401, contentType: 'application/json', body: '{"error":"unauthorized"}' }),
  )
  // Drop the cached list and refetch it under the mocked 401.
  await page.goto('/dashboard/apps?x=1')

  await page.waitForURL(/\/dashboard\/login\?return_to=/)
  const returnTo = new URL(page.url()).searchParams.get('return_to')
  expect(returnTo).toBe('/dashboard/apps?x=1')
})

test('a 401 on the login page itself does not loop', async ({ browser }) => {
  const ctx = await browser.newContext({ storageState: { cookies: [], origins: [] } })
  const page = await ctx.newPage()
  await page.goto('/dashboard/login')
  await expect(page.locator('input[type="email"]')).toBeVisible()
  expect(page.url()).not.toContain('return_to=')
  await ctx.close()
})

test('a 401 that is not an expired session (wrong current password) stays on the page', async ({ page }) => {
  await page.goto('/dashboard/apps')
  await expect(page.getByRole('heading', { name: 'Your apps' })).toBeVisible()
  await page.route(/\/dashboard\/api\/me\/password$/, (route) =>
    route.fulfill({ status: 401, contentType: 'application/json', body: '{"error":"current password is incorrect"}' }),
  )

  await page.getByRole('button', { name: 'Change Password' }).first().click()
  const dialog = page.getByRole('dialog')
  await dialog.locator('input[type="password"]').nth(0).fill('wrong-current')
  await dialog.locator('input[type="password"]').nth(1).fill('newpass123')
  await dialog.locator('input[type="password"]').nth(2).fill('newpass123')
  await dialog.getByRole('button', { name: 'Change password' }).click()

  await expect(dialog.getByText('current password is incorrect')).toBeVisible()
  expect(page.url()).not.toContain('/login')
})
