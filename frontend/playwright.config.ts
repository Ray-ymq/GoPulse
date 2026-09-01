import { defineConfig } from '@playwright/test'

export default defineConfig({
  testDir: './e2e',
  fullyParallel: false,
  workers: 1,
  reporter: 'line',
  use: {
    baseURL: process.env.GOPULSE_BASE_URL ?? 'http://127.0.0.1:5173',
    trace: 'retain-on-failure',
  },
})
