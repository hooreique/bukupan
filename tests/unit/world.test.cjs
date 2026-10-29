const { test } = require('node:test');
const assert = require('node:assert/strict');
const world = require('../../client/world.js');
const close = (a, b) => a.forEach((v, i) => assert.ok(Math.abs(v - b[i]) < 1e-6, `${a} != ${b}`));

test('world coordinates round trip across aspect ratios, negative positions and distant origins', () => {
  for (const [width, height] of [[800, 600], [390, 844], [1800, 500]]) {
    for (const zoom of [.25, 1, 8]) {
      const camera = { x: 1e8, y: -1e8, zoom };
      for (const screen of [[0, 0], [width / 2, height / 2], [width, height]]) {
        close(world.worldToScreen(world.screenToWorld(screen, camera, width, height), camera, width, height), screen);
      }
      close(world.worldToScreen([camera.x + 1, camera.y + 1], camera, width, height), [width / 2 + zoom, height / 2 + zoom]);
    }
  }
});

test('zoom keeps the cursor anchored and bounded view requests stay within the world', () => {
  const camera = { x: -1024, y: 2234, zoom: 1 };
  const anchor = [173, 317];
  const next = world.zoomAt(camera, 4, anchor, 800, 600);
  close(world.screenToWorld(anchor, next, 800, 600), world.screenToWorld(anchor, camera, 800, 600));
  const edge = world.constrain({ x: 1e12, y: -1e12, zoom: .001 }, 800, 600);
  const area = world.viewport(edge, 800, 600);
  assert.ok(area.every(v => Math.abs(v) < world.LIMIT));
  assert.ok(area[2] - area[0] < 32768);
  assert.ok(world.contains(area, [edge.x, edge.y, edge.x, edge.y]));
});
