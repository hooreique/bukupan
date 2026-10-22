"use strict";

const canvas = document.querySelector("canvas");
const context = canvas.getContext("2d", { alpha: false });
const segments = new Map();
const pointers = new Map();
const clientID = Array.from(crypto.getRandomValues(new Uint32Array(4)), (n) => n.toString(16)).join("-");
const maxSegments = 16000;
const maxBatch = 128;
let sequence = 0;
let lifetime = 10000;
let clockOffset = Date.now() - performance.now();
let width = 1;
let height = 1;
let frame = 0;
let pending = [];
let socket;
let ready = false;
let retryTimer;
let retryDelay = 500;
let suspended = false;

const now = () => performance.now() + clockOffset;

function remember(segment) {
  segments.set(segment.id, segment);
  while (segments.size > maxSegments) {
    segments.delete(segments.keys().next().value);
  }
}

function requestDraw() {
  if (!frame) frame = requestAnimationFrame(render);
}

function flush() {
  if (!ready || socket.readyState !== WebSocket.OPEN) {
    pending = [];
    return;
  }
  // Do not accumulate old drawings on a stalled mobile connection.
  if (socket.bufferedAmount > 64 * 1024) {
    pending = [];
    ready = false;
    socket.close();
    return;
  }
  while (pending.length) {
    socket.send(JSON.stringify({ type: "draw", segments: pending.splice(0, maxBatch) }));
  }
}

function render() {
  frame = 0;
  flush();
  const time = now();
  context.globalAlpha = 1;
  context.globalCompositeOperation = "source-over";
  context.fillStyle = "#000";
  context.fillRect(0, 0, width, height);
  context.strokeStyle = "#fff";
  context.fillStyle = "#fff";
  context.lineWidth = 3;
  context.lineCap = "round";
  // 'lighten' keeps overlapping segment caps from leaving bright beads as they fade.
  context.globalCompositeOperation = "lighten";
  const size = Math.max(width, height);
  const offsetX = (width - size) / 2;
  const offsetY = (height - size) / 2;
  for (const [id, segment] of segments) {
    const age = Math.max(0, time - segment.at) / lifetime;
    if (age >= 1) {
      segments.delete(id);
      continue;
    }
    // Smoothstep fades gently at both ends of the ten-second lifetime.
    context.globalAlpha = 1 - age * age * (3 - 2 * age);
    const x1 = segment.x1 * size + offsetX;
    const y1 = segment.y1 * size + offsetY;
    const x2 = segment.x2 * size + offsetX;
    const y2 = segment.y2 * size + offsetY;
    context.beginPath();
    if (x1 === x2 && y1 === y2) {
      context.arc(x1, y1, 1.5, 0, Math.PI * 2);
      context.fill();
    } else {
      context.moveTo(x1, y1);
      context.lineTo(x2, y2);
      context.stroke();
    }
  }
  if (segments.size) requestDraw();
}

function resize() {
  const rect = canvas.getBoundingClientRect();
  width = rect.width;
  height = rect.height;
  const ratio = Math.min(window.devicePixelRatio || 1, 2);
  canvas.width = Math.round(width * ratio);
  canvas.height = Math.round(height * ratio);
  context.setTransform(ratio, 0, 0, ratio, 0, 0);
  pointers.clear();
  requestDraw();
}

function position(event) {
  const rect = canvas.getBoundingClientRect();
  // Share a centered square; the viewport crops its edges without stretching it.
  const size = Math.max(rect.width, rect.height);
  return {
    x: Math.max(0, Math.min(1, (event.clientX - rect.left + (size - rect.width) / 2) / size)),
    y: Math.max(0, Math.min(1, (event.clientY - rect.top + (size - rect.height) / 2) / size)),
  };
}

function draw(from, to) {
  const segment = {
    id: `${clientID}:${sequence++}`,
    x1: from.x, y1: from.y, x2: to.x, y2: to.y, at: Math.round(now()),
  };
  remember(segment);
  if (ready) {
    pending.push(segment);
    if (pending.length >= maxBatch) flush();
  }
  requestDraw();
}

canvas.addEventListener("pointerdown", (event) => {
  if (event.button !== 0) return;
  event.preventDefault();
  canvas.setPointerCapture(event.pointerId);
  const point = position(event);
  pointers.set(event.pointerId, point);
  draw(point, point);
});

function move(event) {
  let previous = pointers.get(event.pointerId);
  if (!previous) return;
  event.preventDefault();
  const samples = event.getCoalescedEvents?.() || [];
  for (const sample of samples.length ? samples : [event]) {
    const point = position(sample);
    if (point.x !== previous.x || point.y !== previous.y) draw(previous, point);
    previous = point;
  }
  pointers.set(event.pointerId, previous);
}

canvas.addEventListener("pointermove", move);
canvas.addEventListener("pointerup", (event) => {
  move(event);
  pointers.delete(event.pointerId);
});
for (const type of ["pointercancel", "lostpointercapture"]) {
  canvas.addEventListener(type, (event) => pointers.delete(event.pointerId));
}
canvas.addEventListener("contextmenu", (event) => event.preventDefault());
window.addEventListener("blur", () => pointers.clear());

function connect() {
  clearTimeout(retryTimer);
  if (suspended || (socket && socket.readyState < WebSocket.CLOSING)) return;
  const url = new URL("/ws", location.href);
  url.protocol = location.protocol === "https:" ? "wss:" : "ws:";
  const connection = new WebSocket(url);
  socket = connection;

  connection.addEventListener("message", (event) => {
    if (connection !== socket) return;
    const message = JSON.parse(event.data);
    if (message.type === "snapshot") {
      clockOffset = message.now - performance.now();
      lifetime = message.lifetime;
      segments.clear();
      pending = [];
      ready = true;
      retryDelay = 500;
    }
    for (const segment of message.segments) remember(segment);
    requestDraw();
  });
  connection.addEventListener("close", () => {
    if (connection !== socket) return;
    ready = false;
    pending = [];
    if (!suspended) {
      retryTimer = setTimeout(connect, retryDelay + Math.random() * 250);
      retryDelay = Math.min(retryDelay * 2, 5000);
    }
  });
  connection.addEventListener("error", () => connection.close());
}

window.addEventListener("online", connect);
window.addEventListener("pagehide", () => {
  suspended = true;
  ready = false;
  pointers.clear();
  clearTimeout(retryTimer);
  socket?.close();
});
window.addEventListener("pageshow", () => {
  suspended = false;
  connect();
});
window.addEventListener("resize", resize);
window.visualViewport?.addEventListener("resize", resize);
resize();
connect();
