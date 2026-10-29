"use strict";

// Document coordinates remain doubles. Convert to camera-relative CSS pixels
// before sending anything to a float32 GPU buffer.
const BoardWorld = (() => {
  const LIMIT = 1e9;
  function screenToWorld(p, camera, width, height) {
    return [camera.x + (p[0] - width / 2) / camera.zoom,
      camera.y + (p[1] - height / 2) / camera.zoom];
  }
  function worldToScreen(p, camera, width, height) {
    return [(p[0] - camera.x) * camera.zoom + width / 2,
      (p[1] - camera.y) * camera.zoom + height / 2];
  }
  function constrain(camera, width, height) {
    camera.zoom = Math.max(.25, Math.max(width, height) / 24000, Math.min(8, camera.zoom));
    const margin = Math.max(width, height) / camera.zoom + 1024;
    camera.x = Math.max(-LIMIT + margin, Math.min(LIMIT - margin, camera.x));
    camera.y = Math.max(-LIMIT + margin, Math.min(LIMIT - margin, camera.y));
    return camera;
  }
  function zoomAt(camera, zoom, anchor, width, height) {
    const before = screenToWorld(anchor, camera, width, height);
    const next = constrain({ ...camera, zoom }, width, height);
    const after = screenToWorld(anchor, next, width, height);
    next.x += before[0] - after[0]; next.y += before[1] - after[1];
    return constrain(next, width, height);
  }
  function viewport(camera, width, height) {
    const a = screenToWorld([-128, -128], camera, width, height);
    const b = screenToWorld([width + 128, height + 128], camera, width, height);
    return [...a, ...b];
  }
  function contains(outer, inner) {
    return outer && outer[0] <= inner[0] && outer[1] <= inner[1] && outer[2] >= inner[2] && outer[3] >= inner[3];
  }
  function lines(points) {
    if (points.length === 1) return [[...points[0], ...points[0]]];
    return points.slice(1).map((p, i) => [...points[i], ...p]);
  }
  return { LIMIT, screenToWorld, worldToScreen, constrain, zoomAt, viewport, contains, lines };
})();
if (typeof module !== "undefined") module.exports = BoardWorld;
