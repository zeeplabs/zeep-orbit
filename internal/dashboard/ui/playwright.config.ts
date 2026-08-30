import { defineConfig, devices } from '@playwright/test'
import { STORAGE_STATE_PATH } from './e2e/storage-state'

export default defineConfig({
  testDir: './e2e',
  fullyParallel: false,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  workers: 1,
  reporter: 'html',
  timeout: 30000,
  // Logs in once and reuses that session across every project/spec via
  // `storageState` below -- avoids ~20 fresh POST /api/login calls tripping
  // the backend's per-IP login rate limiter. auth.spec.ts opts out per-file
  // (test.use({ storageState: ... })) since it tests the login flow itself.
  globalSetup: './e2e/global-setup',
  use: {
    baseURL: process.env.BASE_URL || 'http://localhost:8080/dashboard',
    storageState: STORAGE_STATE_PATH,
    trace: 'on-first-retry',
    screenshot: 'only-on-failure',
  },
  projects: [
    {
      // responsive-nav.spec.ts's tests each self-skip via
      // `test.skip(testInfo.project.name !== '<project>', ...)`, so this
      // project only actually executes the one test scoped to `chromium`
      // (the sub-1920px-cap content-width check) -- the other 5 skip here,
      // same as `chromium`'s tests skip under mobile/tablet/ultrawide.
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
    {
      // Scoped to responsive-nav.spec.ts only: the existing suite isn't written
      // to be viewport-agnostic, so letting it run under every project here
      // would 4x CI time and risk false failures unrelated to responsiveness.
      name: 'mobile',
      testMatch: /responsive-nav\.spec\.ts/,
      use: { ...devices['Desktop Chrome'], viewport: { width: 390, height: 844 } },
    },
    {
      name: 'tablet',
      testMatch: /responsive-nav\.spec\.ts/,
      use: { ...devices['Desktop Chrome'], viewport: { width: 820, height: 1180 } },
    },
    {
      name: 'ultrawide',
      testMatch: /responsive-nav\.spec\.ts/,
      use: { ...devices['Desktop Chrome'], viewport: { width: 2560, height: 1440 } },
    },
  ],
})
