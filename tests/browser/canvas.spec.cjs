const { test, expect } = require('./fixtures.cjs');

test('client state: sharing, echo merge, reconnect, expiration and invalid data', async ({ page, appURL }) => {
  await page.goto(`${appURL}/__tests/scripts/test-client.html`);
  const result = page.locator('#result');
  await expect(result).toHaveText(/^(PASS|FAIL)/, { timeout: 20_000 });
  await expect(result).toHaveText(/^PASS/);
});

test('WebGL2 pixels: joins, dots, instance capacity and expiration', async ({ page, appURL }) => {
  await page.goto(`${appURL}/__tests/scripts/test-renderer.html`);
  const result = page.locator('#result');
  await expect(result).toHaveText(/^(PASS|FAIL)/);
  await expect(result).toHaveText(/^PASS/);
});

async function openDrawing(page, url) {
  // Wait for the actual initial snapshot, not just an open WebSocket.
  await Promise.all([
    page.waitForEvent('websocket', { timeout: 5_000 })
      .then(socket => socket.waitForEvent('framereceived', { timeout: 5_000 })),
    (async () => {
      await page.goto(url);
      await expect(page.locator('canvas')).toBeVisible();
      await expect(page.locator('.unsupported')).toHaveCount(0);
    })(),
  ]);
}

test('real pointer input draws on both independent sessions and reload restores it', async ({ page, otherPage, appURL }) => {
  await Promise.all([openDrawing(page, appURL), openDrawing(otherPage, appURL)]);
  const canvas = page.locator('canvas');
  const remoteCanvas = otherPage.locator('canvas');
  const empty = await canvas.screenshot();
  const remoteEmpty = await remoteCanvas.screenshot();

  // Native input exercises the production pointer capture and WebGL renderer.
  await page.mouse.move(160, 240);
  await page.mouse.down();
  await page.mouse.move(480, 240, { steps: 12 });
  await page.mouse.up();
  await expect.poll(async () => (await canvas.screenshot()).equals(empty), {
    message: 'Local canvas contains the new stroke',
  }).toBe(false);
  await expect.poll(async () => (await remoteCanvas.screenshot()).equals(remoteEmpty), {
    message: 'Other participant sees the stroke',
  }).toBe(false);

  await openDrawing(otherPage, appURL);
  await expect.poll(async () => (await otherPage.locator('canvas').screenshot()).equals(remoteEmpty), {
    message: 'Reload restores the server snapshot',
  }).toBe(false);
});
