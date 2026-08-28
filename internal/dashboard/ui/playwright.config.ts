import { defineConfig, devices } from '@playwright/test'

export default defineConfig({
  testDir: './e2e',
  fullyParallel: false,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  workers: 1,
  reporter: 'html',
  timeout: 30000,
  use: {
    baseURL: process.env.BASE_URL || 'http://localhost:8080/dashboard',
    trace: 'on-first-retry',
    screenshot: 'only-on-failure',
  },
  projects: [
    {
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
