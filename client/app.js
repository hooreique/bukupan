"use strict";

(async () => {
  const canvas = document.querySelector("canvas");
  const status = document.querySelector("#status");
  const undo = document.querySelector("#undo");
  const recovery = document.querySelector("#recovery");
  const cursor = document.querySelector("#eraser-cursor");
  const world = BoardWorld, codec = BoardCodec;
  let width = innerWidth, height = innerHeight;
  let camera = { x: 0, y: 0, zoom: 1 };
  let renderer, frame = 0, tool = "pen", space = false, gesture = null, pinch = null;
  let db, author, board, socket, connected = false, ready = false;
  let generation = 0, waitingView = false, dirtyView = false, requestedArea;
  let viewTimer, cameraTimer, ackTimer, reconnectTimer, retryDelay = 500;
  let history = [], problem = null, inFlight = null, order = Date.now();
  let persistence = Promise.resolve(), messages = Promise.resolve();
  const drawings = new Map(), pending = new Map(), pointers = new Map();
  const explanations = {
    local: "기기에 작업을 보관하지 못했어요. 저장 공간을 확인해 주세요.",
    storage: "서버가 저장하지 못했어요. 미전송 작업은 이 기기에 남아 있어요.",
    dense: "이곳에 선이 많아요. 더 확대해서 열어 주세요.",
    invalid: "저장하지 못한 작업이 있어요. 내보내거나 제외한 뒤 계속할 수 있어요.",
    board: "다른 그림판의 미전송 작업이 이 기기에 남아 있어요.",
    protocol: "서버와 연결할 수 없어요. 페이지를 새로고침해 주세요.",
  };
  function updateStatus() {
    let message = problem ? explanations[problem] : !connected ? "연결을 기다리고 있어요" : !ready || waitingView || dirtyView ? "그림을 불러오는 중" : pending.size || gesture?.dirty ? "저장 중…" : "저장됨";
    if (!connected && pending.size && !problem) message = `연결 대기 · 미전송 작업 ${pending.size}개 보관 중`;
    status.textContent = message;
    status.dataset.state = problem ? "error" : ready && !waitingView && !dirtyView && !pending.size && !gesture?.dirty ? "saved" : "waiting";
    recovery.hidden = !problem && pending.size === 0;
    document.querySelector("#export").hidden = pending.size === 0;
    document.querySelector("#discard").hidden = !problem || pending.size === 0;
    document.querySelector("#retry").hidden = !problem;
    undo.disabled = !canEdit() || !history.length || pending.size > 0 || !!gesture;
    document.querySelector("#zoom-level").textContent = `${Math.round(camera.zoom * 100)}%`;
    canvas.dataset.tool = space ? "move" : tool;
  }
  function canEdit() { return ready && connected && !waitingView && !dirtyView && !problem && pending.size < 128; }
  function preview() {
    const result = [...drawings.values()];
    for (const record of pending.values()) {
      if (record.board === board && record.op.kind === "pen" && !drawings.has(record.id)) result.push({ width: record.op.width, lines: world.lines(record.op.points) });
    }
    if (gesture?.kind === "pen" && gesture.dirty) result.push({ width: gesture.width, lines: world.lines(gesture.points) });
    return result;
  }
  function redraw() {
    if (frame) return;
    frame = requestAnimationFrame(() => { frame = 0; renderer.render(preview(), camera, width, height); });
  }
  function changedCamera() {
    if (problem === "dense") problem = null;
    dirtyView = true;
    world.constrain(camera, width, height); redraw(); updateStatus();
    clearTimeout(cameraTimer);
    cameraTimer = setTimeout(() => { try { localStorage.setItem("bukupan.camera", JSON.stringify(camera)); } catch { /* Camera preferences are optional. */ } }, 200);
    clearTimeout(viewTimer); viewTimer = setTimeout(loadView, 80);
  }
  function loadView() {
    if (!connected || waitingView) return;
    requestedArea = world.viewport(camera, width, height);
    waitingView = true; dirtyView = false; generation++;
    socket.send(codec.encode({ type: "view", board, author, view: generation, area: requestedArea }));
    updateStatus();
  }
  function drain() {
    if (!ready || !connected || inFlight || problem) return;
    const record = pending.values().next().value;
    if (!record || !record.persisted) return;
    if (record.board !== board) { problem = "board"; updateStatus(); return; }
    inFlight = record.id;
    socket.send(codec.encode({ type: "edit", board, edit: record.op }));
    clearTimeout(ackTimer);
    ackTimer = setTimeout(() => socket?.close(), 15000);
  }
  function persist(record) {
    persistence = persistence.then(async () => {
      await db.put({ id: record.id, board: record.board, order: record.order, op: record.op });
      record.persisted = true; drain(); updateStatus();
    }).catch(() => { problem = "local"; updateStatus(); });
  }
  function enqueue(op) {
    const record = { id: op.id, board, order: ++order, op, persisted: false };
    pending.set(record.id, record); persist(record); redraw(); updateStatus();
  }
  function refreshDrawings(message) {
    if (message.type === "snapshot") drawings.clear();
    for (const drawing of message.drawings) {
      if (drawing.lines.length) drawings.set(drawing.id, drawing); else drawings.delete(drawing.id);
    }
    let lines = 0;
    for (const drawing of drawings.values()) lines += drawing.lines.length;
    if (drawings.size > codec.MAX_DRAWINGS || lines > codec.MAX_LINES) {
      // Never evict old document data to make space. A smaller view can load it.
      drawings.clear(); problem = "dense"; ready = false;
    }
    redraw();
  }
  async function receive(message) {
    if (message.type === "hello") {
      board = message.board; connected = true; retryDelay = 500;
      if ([...pending.values()].some(record => record.board !== board)) problem = "board";
      loadView();
    } else if (message.type === "snapshot" && message.view === generation) {
      waitingView = false; ready = true;
      refreshDrawings(message); history = message.history || [];
      if (dirtyView) loadView();
      drain();
    } else if (message.type === "patch" && message.view === generation) {
      refreshDrawings(message);
      if (message.history) history = message.history;
    } else if (message.type === "ack") {
      // The same receipt may arrive after reconnect or in another tab.
      if (pending.has(message.id)) {
        try { await db.remove(message.id); } catch { problem = "local"; updateStatus(); return; }
        pending.delete(message.id);
      }
      if (inFlight === message.id) { clearTimeout(ackTimer); inFlight = null; }
      history = message.history || []; redraw(); drain();
    } else if (message.type === "error") {
      if (message.view && message.view !== generation) return;
      problem = message.id && message.code === "dense" ? "invalid" : message.code;
      if (message.view) {
        waitingView = false; ready = false;
        if (message.code === "dense") { drawings.clear(); redraw(); }
      }
      clearTimeout(ackTimer); inFlight = null;
      if (dirtyView && message.view) { problem = null; loadView(); }
    }
    updateStatus();
  }
  function connect() {
    clearTimeout(reconnectTimer);
    const next = new WebSocket(`${location.protocol === "https:" ? "wss:" : "ws:"}//${location.host}/ws`);
    socket = next; next.binaryType = "arraybuffer";
    next.onmessage = event => {
      messages = messages.then(() => {
        if (socket === next) return receive(codec.decode(event.data));
      }).catch(() => { problem = "protocol"; updateStatus(); next.close(); });
    };
    next.onclose = () => {
      if (socket !== next) return;
      finishGesture(); connected = ready = waitingView = false;
      inFlight = null; clearTimeout(ackTimer); updateStatus();
      reconnectTimer = setTimeout(connect, retryDelay); retryDelay = Math.min(10000, retryDelay * 1.7);
    };
  }
  function resize() {
    const ratio = Math.min(devicePixelRatio || 1, 3);
    if (width === innerWidth && height === innerHeight && canvas.width === Math.round(width * ratio) && canvas.height === Math.round(height * ratio)) { redraw(); return; }
    width = innerWidth; height = innerHeight;
    canvas.width = Math.round(width * ratio); canvas.height = Math.round(height * ratio);
    changedCamera();
  }
  function locationOf(event) { return [event.clientX, event.clientY]; }
  function worldPoint(event) { return world.screenToWorld(locationOf(event), camera, width, height); }
  function flushGesture(final = false) {
    if (!gesture || !["pen", "erase"].includes(gesture.kind) || !gesture.dirty) return;
    if (gesture.touch && !gesture.started && !final) return;
    const points = gesture.points;
    enqueue({ id: codec.randomID(), author, action: gesture.action, kind: gesture.kind, width: gesture.width, points });
    gesture.points = [points[points.length - 1]]; gesture.dirty = false; gesture.committed = true;
  }
  function finishGesture() { flushGesture(true); gesture = null; redraw(); updateStatus(); }
  function appendPoint(p) {
    if (!gesture || !canEdit()) return;
    const last = gesture.points[gesture.points.length - 1];
    if (Math.hypot(p[0] - last[0], p[1] - last[1]) * camera.zoom < .5) return;
    // Subdivide unusually long pointer jumps and keep every request bounded.
    const steps = Math.ceil(Math.max(Math.abs(p[0] - last[0]), Math.abs(p[1] - last[1])) / 3000);
    for (let step = 1; step <= steps; step++) {
      const next = [last[0] + (p[0] - last[0]) * step / steps, last[1] + (p[1] - last[1]) * step / steps];
      const first = gesture.points[0];
      if (gesture.points.length >= 64 || Math.abs(next[0] - first[0]) > 6000 || Math.abs(next[1] - first[1]) > 6000) flushGesture();
      if (pending.size >= 128) break;
      gesture.points.push(next); gesture.dirty = true;
    }
    redraw(); updateStatus();
  }
  function beginPinch() {
    // A second finger arriving before drawing starts never leaves an accidental dot.
    gesture = null; redraw();
    const [a, b] = [...pointers.values()];
    const center = [(a[0] + b[0]) / 2, (a[1] + b[1]) / 2];
    pinch = { anchor: world.screenToWorld(center, camera, width, height), zoom: camera.zoom, distance: Math.max(1, Math.hypot(a[0] - b[0], a[1] - b[1])) };
  }
  canvas.addEventListener("pointerdown", event => {
    if (event.button !== 0 && event.button !== 1) return;
    event.preventDefault(); canvas.setPointerCapture(event.pointerId);
    pointers.set(event.pointerId, locationOf(event));
    if (pointers.size === 2) { beginPinch(); return; }
    if (pointers.size !== 1) return;
    if (space || tool === "move" || event.button === 1) {
      gesture = { kind: "move", pointer: event.pointerId, previous: locationOf(event) }; return;
    }
    if (!canEdit()) return;
    gesture = { kind: tool, pointer: event.pointerId, action: codec.randomID(), width: tool === "erase" ? 28 / camera.zoom : 3,
      points: [worldPoint(event)], dirty: true, touch: event.pointerType === "touch", started: event.pointerType !== "touch", origin: locationOf(event) };
    redraw(); updateStatus();
  });
  canvas.addEventListener("pointermove", event => {
    cursor.style.left = `${event.clientX}px`; cursor.style.top = `${event.clientY}px`;
    cursor.hidden = tool !== "erase" || space || event.pointerType === "touch";
    if (!pointers.has(event.pointerId)) return;
    pointers.set(event.pointerId, locationOf(event));
    if (pinch && pointers.size >= 2) {
      const [a, b] = [...pointers.values()];
      const center = [(a[0] + b[0]) / 2, (a[1] + b[1]) / 2];
      camera.zoom = pinch.zoom * Math.hypot(a[0] - b[0], a[1] - b[1]) / pinch.distance;
      world.constrain(camera, width, height);
      camera.x = pinch.anchor[0] - (center[0] - width / 2) / camera.zoom;
      camera.y = pinch.anchor[1] - (center[1] - height / 2) / camera.zoom;
      changedCamera(); return;
    }
    if (!gesture || gesture.pointer !== event.pointerId) return;
    if (gesture.kind === "move") {
      camera.x -= (event.clientX - gesture.previous[0]) / camera.zoom;
      camera.y -= (event.clientY - gesture.previous[1]) / camera.zoom;
      gesture.previous = locationOf(event); changedCamera(); return;
    }
    if (!gesture.started) {
      if (Math.hypot(event.clientX - gesture.origin[0], event.clientY - gesture.origin[1]) < 6) return;
      gesture.started = true;
    }
    const samples = event.getCoalescedEvents?.() || [];
    for (const sample of samples.length ? samples : [event]) appendPoint(worldPoint(sample));
  });
  function endPointer(event) {
    if (gesture?.pointer === event.pointerId) finishGesture();
    pointers.delete(event.pointerId);
    // Remaining fingers must lift before beginning another stroke after a pinch.
    if (pinch && pointers.size === 0) { pinch = null; gesture = null; }
    if (canvas.hasPointerCapture(event.pointerId)) canvas.releasePointerCapture(event.pointerId);
  }
  canvas.addEventListener("pointerup", endPointer);
  canvas.addEventListener("pointercancel", endPointer);
  canvas.addEventListener("lostpointercapture", event => { if (pointers.has(event.pointerId)) endPointer(event); });
  canvas.addEventListener("pointerleave", () => { cursor.hidden = true; });
  canvas.addEventListener("contextmenu", event => event.preventDefault());
  function zoomTo(value, anchor = [width / 2, height / 2]) {
    finishGesture(); camera = world.zoomAt(camera, value, anchor, width, height);
    if (problem === "dense") problem = null;
    changedCamera();
  }
  canvas.addEventListener("wheel", event => { event.preventDefault(); zoomTo(camera.zoom * Math.exp(-Math.max(-300, Math.min(300, event.deltaY)) * .002), locationOf(event)); }, { passive: false });
  function selectTool(value) {
    finishGesture(); tool = value; cursor.hidden = true;
    for (const button of document.querySelectorAll("[data-tool]")) if (button.tagName === "BUTTON") button.setAttribute("aria-pressed", String(button.dataset.tool === tool));
    updateStatus();
  }
  for (const button of document.querySelectorAll("button[data-tool]")) button.addEventListener("click", () => selectTool(button.dataset.tool));
  function undoAction() {
    if (canEdit() && !pending.size && !gesture && history.length) enqueue({ id: codec.randomID(), author, kind: "undo", target: history[0] });
  }
  undo.addEventListener("click", undoAction);
  document.querySelector("#zoom-in").addEventListener("click", () => zoomTo(camera.zoom * 1.4));
  document.querySelector("#zoom-out").addEventListener("click", () => zoomTo(camera.zoom / 1.4));
  document.querySelector("#home").addEventListener("click", () => { finishGesture(); camera = { x: 0, y: 0, zoom: 1 }; if (problem === "dense") problem = null; changedCamera(); });
  window.addEventListener("keydown", event => {
    if (event.target.matches("input, textarea, [contenteditable]")) return;
    if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "z") { event.preventDefault(); undoAction(); return; }
    if (event.ctrlKey || event.metaKey || event.altKey) return;
    if (event.code === "Space" && event.target === document.body) { event.preventDefault(); if (!space) finishGesture(); space = true; updateStatus(); }
    if (event.key.toLowerCase() === "b") selectTool("pen");
    if (event.key.toLowerCase() === "e") selectTool("erase");
    if (event.key.toLowerCase() === "h") selectTool("move");
    if (event.key === "0") document.querySelector("#home").click();
    if (event.key === "+" || event.key === "=") zoomTo(camera.zoom * 1.4);
    if (event.key === "-") zoomTo(camera.zoom / 1.4);
  });
  window.addEventListener("keyup", event => { if (event.code === "Space") { space = false; updateStatus(); } });
  window.addEventListener("blur", () => { finishGesture(); space = false; pointers.clear(); pinch = null; updateStatus(); });
  document.addEventListener("visibilitychange", () => { if (document.hidden) finishGesture(); });
  window.addEventListener("beforeunload", event => {
    finishGesture();
    if ([...pending.values()].some(record => !record.persisted)) { event.preventDefault(); event.returnValue = ""; }
  });
  document.querySelector("#retry").addEventListener("click", () => {
    problem = null;
    for (const record of pending.values()) if (!record.persisted) persist(record);
    if (socket?.readyState === WebSocket.OPEN) socket.close(); else connect();
    updateStatus();
  });
  document.querySelector("#export").addEventListener("click", () => {
    const blob = new Blob([JSON.stringify({ version: 2, pending: [...pending.values()].map(({ persisted, ...record }) => record) }, null, 2)], { type: "application/json" });
    const url = URL.createObjectURL(blob), link = document.createElement("a");
    link.href = url; link.download = "bukupan-unsent.json"; link.click(); setTimeout(() => URL.revokeObjectURL(url), 1000);
  });
  document.querySelector("#discard").addEventListener("click", async () => {
    if (!confirm("이 기기의 미전송 작업을 제외할까요? 필요하면 먼저 작업을 내보내세요. 이미 서버에 저장된 그림은 그대로 남습니다.")) return;
    await persistence;
    try { for (const id of pending.keys()) await db.remove(id); pending.clear(); problem = null; redraw(); socket?.close(); }
    catch { problem = "local"; }
    updateStatus();
  });
  try {
    renderer = createStrokeRenderer(canvas, codec.MAX_LINES + 128 * 128 + 128, redraw);
    if (!renderer) throw new Error("WebGL2");
  } catch {
    document.querySelector("#controls").hidden = true;
    status.textContent = "이 그림판은 WebGL2를 지원하는 브라우저가 필요해요.";
    return;
  }
  try {
    author = localStorage.getItem("bukupan.author");
    if (!codec.id(author)) { author = codec.randomID(); localStorage.setItem("bukupan.author", author); }
    try {
      const saved = JSON.parse(localStorage.getItem("bukupan.camera"));
      if (saved && [saved.x, saved.y, saved.zoom].every(Number.isFinite) && saved.zoom > 0) camera = saved;
    } catch { /* Ignore a damaged optional camera preference. */ }
    db = await BoardOutbox.open();
    const records = await db.all();
    records.sort((a, b) => a.order - b.order);
    if (records.length && records.every(record => record.op.author === records[0].op.author)) {
      author = records[0].op.author; localStorage.setItem("bukupan.author", author);
    }
    for (const record of records) { pending.set(record.id, { ...record, persisted: true }); order = Math.max(order, record.order); }
  } catch {
    problem = "local"; updateStatus();
    document.querySelector("#retry").onclick = () => location.reload();
    return;
  }
  window.addEventListener("resize", resize);
  setInterval(() => flushGesture(), 80);
  resize(); connect(); updateStatus();
})();
