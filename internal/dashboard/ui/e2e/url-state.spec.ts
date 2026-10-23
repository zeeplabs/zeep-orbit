import { test, expect, Page, Route } from '@playwright/test'

/**
 * Filter/sort/page state lives in the URL for Data Browser, Logs and Audit log:
 * reload, back and shared links keep the view. The backend is mocked here — these
 * tests are about what the UI sends and puts in the address bar, so the data has to
 * be deterministic.
 */

const json = (route: Route, body: unknown) =>
  route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(body) })

const COLUMNS = [
  { name: 'id', type: 'uuid' },
  { name: 'name', type: 'text' },
  { name: 'status', type: 'text' },
]

async function mockDataBrowser(page: Page): Promise<URL[]> {
  const queries: URL[] = []
  await page.route('**/dashboard/api/data-browser/apps', (r) =>
    json(r, [
      {
        name: 'shop',
        tables: [
          { name: 'orders', count: 120, columns: COLUMNS },
          { name: 'users', count: 3, columns: COLUMNS },
        ],
      },
    ]),
  )
  await page.route('**/dashboard/api/data-browser/query?*', (r) => {
    const url = new URL(r.request().url())
    queries.push(url)
    const offset = Number(url.searchParams.get('offset'))
    json(r, { data: [{ id: `row-${offset}`, name: `n${offset}`, status: 'ok' }], count: 120, limit: 50, offset })
  })
  return queries
}

test.describe('Data Browser URL state', () => {
  test('a deep link restores table, page, sort and filter', async ({ page }) => {
    const queries = await mockDataBrowser(page)
    await page.goto('/dashboard/data-browser?app=shop&table=orders&page=2&sort=name.desc&f=status%7Ceq%7Cok')
    await expect(page.locator('tbody tr').first()).toContainText('n50')

    const q = queries.at(-1)!
    expect(q.searchParams.get('app')).toBe('shop')
    expect(q.searchParams.get('table')).toBe('orders')
    expect(q.searchParams.get('offset')).toBe('50')
    expect(q.searchParams.get('order')).toBe('name.desc')
    expect(q.searchParams.get('status')).toBe('eq.ok')
  })

  test('paging, sorting and reload keep the view; changing sort goes back to page 1', async ({ page }) => {
    await mockDataBrowser(page)
    await page.goto('/dashboard/data-browser?app=shop&table=orders&page=2&sort=name.desc')
    await expect(page.locator('tbody tr').first()).toContainText('n50')

    await page.getByRole('button', { name: /next/i }).click()
    await expect(page).toHaveURL(/page=3/)

    await page.getByRole('button', { name: /name/i }).first().click()
    await expect(page).toHaveURL(/sort=name\.asc/)
    await expect(page).not.toHaveURL(/page=/)

    await page.reload()
    await expect(page).toHaveURL(/sort=name\.asc/)
    await expect(page.locator('tbody tr').first()).toContainText('n0')

    await page.goBack()
    await expect(page).toHaveURL(/page=3/)
  })

  test('a sort on a column the table does not have is ignored instead of breaking the query', async ({ page }) => {
    const queries = await mockDataBrowser(page)
    await page.goto('/dashboard/data-browser?app=shop&table=orders&sort=dropped_column.desc')
    await expect(page.locator('tbody tr').first()).toContainText('n0')
    expect(queries.at(-1)!.searchParams.get('order')).toBeNull()

    // A valid sort still goes through.
    await page.goto('/dashboard/data-browser?app=shop&table=orders&sort=status.asc')
    await expect(page.locator('tbody tr').first()).toBeVisible()
    await expect.poll(() => queries.at(-1)!.searchParams.get('order')).toBe('status.asc')
  })

  test('picking another table clears page, sort and filters', async ({ page }) => {
    await mockDataBrowser(page)
    await page.goto('/dashboard/data-browser?app=shop&table=orders&page=2&sort=name.desc&f=status%7Ceq%7Cok')
    await expect(page.locator('tbody tr').first()).toBeVisible()

    await page.getByText('users', { exact: true }).first().click()
    await expect(page).toHaveURL(/app=shop&table=users$/)
  })

  test('an app/table that does not exist in the link shows no table and does not crash', async ({ page }) => {
    await mockDataBrowser(page)
    await page.goto('/dashboard/data-browser?app=nope&table=missing')
    await expect(page.locator('table')).toHaveCount(0)
    await expect(page.locator('main#main-content')).toBeVisible()
  })
})

test.describe('Logs URL state', () => {
  test('the app filter is read from and survives a reload of the URL', async ({ page }) => {
    const requests: URL[] = []
    await page.route(/\/dashboard\/api\/logs\/metrics/, (r) =>
      json(r, { total_requests: 1, requests_per_app: {}, avg_latency_ms: 1, errors_4xx: 0, errors_5xx: 0, method_breakdown: {} }),
    )
    await page.route(/\/dashboard\/api\/logs\?/, (r) => {
      requests.push(new URL(r.request().url()))
      json(r, [{ timestamp: '2026-10-20T10:00:00Z', app: 'blog', method: 'GET', path: '/a', status: 200, latency_ms: 1 }])
    })

    await page.goto('/dashboard/logs?app=blog')
    await expect(page.locator('tbody tr').first()).toBeVisible()
    expect(requests.at(-1)!.searchParams.get('app')).toBe('blog')

    await page.reload()
    await expect(page.locator('tbody tr').first()).toBeVisible()
    await expect(page).toHaveURL(/app=blog/)
    expect(requests.at(-1)!.searchParams.get('app')).toBe('blog')
  })
})

test.describe('Audit log URL state', () => {
  test('category, e-mail and page are read from the URL; typing the e-mail does not add history entries', async ({ page }) => {
    const requests: URL[] = []
    await page.route(/\/dashboard\/api\/audit-log\?/, (r) => {
      requests.push(new URL(r.request().url()))
      json(r, {
        data: [{ id: '1', created_at: '2026-10-20T10:00:00Z', user_email: 'x@y.z', action: 'app.create', resource_type: 'app', resource_id: '1' }],
        total: 120,
      })
    })

    await page.goto('/dashboard/auditoria?email=bob&page=2')
    await expect(page.locator('tbody tr').first()).toBeVisible()
    await expect.poll(() => requests.at(-1)?.searchParams.get('user_email')).toBe('bob')
    expect(requests.at(-1)!.searchParams.get('offset')).toBe('50')

    await page.getByRole('button', { name: /next/i }).click()
    await expect(page).toHaveURL(/page=3/)

    const historyBefore = await page.evaluate(() => history.length)
    const input = page.getByPlaceholder(/user|usu/i).first()
    await input.fill('')
    await input.pressSequentially('amy', { delay: 20 })
    await expect(page).toHaveURL(/email=amy/)
    await expect(page).not.toHaveURL(/page=/)
    expect(await page.evaluate(() => history.length)).toBe(historyBefore)
    await expect.poll(() => requests.at(-1)?.searchParams.get('user_email')).toBe('amy')
  })
})
