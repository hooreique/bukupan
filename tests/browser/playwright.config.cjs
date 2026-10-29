const path = require('node:path');
const { defineConfig } = require('@playwright/test');

module.exports = defineConfig({
  testDir: __dirname,
  testMatch: '*.spec.cjs',
  outputDir: path.resolve(__dirname, '../../test-results/browser'),
  reporter: [['list'], ['html', {
    outputFolder: path.resolve(__dirname, '../../test-results/report'),
    open: 'never',
  }]],
  forbidOnly: !!process.env.CI,
  retries: 0,
  workers: 1,
  timeout: 30_000,
  expect: { timeout: 5_000 },
  use: {
    browserName: 'chromium',
    viewport: { width: 640, height: 480 },
    deviceScaleFactor: 1,
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
    launchOptions: {
      // Exercise real WebGL2 without depending on the CI host's GPU.
      args: ['--use-angle=swiftshader', '--enable-unsafe-swiftshader'],
      env: {
        ...process.env,
        ...(process.env.BUKUPAN_BROWSER_LIBRARY_PATH ? {
          LD_LIBRARY_PATH: process.env.BUKUPAN_BROWSER_LIBRARY_PATH,
        } : {}),
      },
    },
  },
});
