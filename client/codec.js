"use strict";

const strokeCodec = (() => {
  const header = 16, record = 36;
  function key(id, sequence) {
    return Array.from(id, b => b.toString(16).padStart(2, "0")).join("") + ":" + sequence;
  }
  function encodeDraw(segments) {
    if (!segments.length || segments.length > 128) throw new Error("Invalid draw count");
    const buffer = new ArrayBuffer(header + record * segments.length);
    const view = new DataView(buffer);
    view.setUint8(0, 1);
    view.setUint8(1, 1);
    view.setUint16(2, segments.length, true);
    segments.forEach((s, i) => {
      const offset = header + record * i;
      new Uint8Array(buffer, offset, 16).set(s.clientID);
      view.setUint32(offset + 16, s.sequence, true);
      [s.x1, s.y1, s.x2, s.y2].forEach((v, j) => {
        if (!Number.isFinite(v) || v < 0 || v > 1) throw new Error("Invalid coordinate");
        view.setFloat32(offset + 20 + 4 * j, v, true);
      });
    });
    return buffer;
  }
  function decode(buffer) {
    if (!(buffer instanceof ArrayBuffer) || buffer.byteLength < header) throw new Error("Invalid message");
    const view = new DataView(buffer);
    const kind = view.getUint8(1), count = view.getUint16(2, true);
    const snapshot = kind === 3, size = snapshot ? 44 : record;
    const time = offset => {
      const value = Number(view.getBigUint64(offset, true));
      if (!Number.isSafeInteger(value)) throw new Error("Invalid timestamp");
      return value;
    };
    const now = time(4), lifetime = view.getUint32(12, true);
    if (view.getUint8(0) !== 1 || (kind !== 2 && !snapshot) ||
        (snapshot ? count > 16000 || lifetime === 0 : count < 1 || count > 128 || lifetime !== 0) ||
        buffer.byteLength !== header + count * size) throw new Error("Invalid header");
    const segments = [];
    for (let i = 0; i < count; i++) {
      const offset = header + size * i;
      const clientID = new Uint8Array(buffer.slice(offset, offset + 16));
      const sequence = view.getUint32(offset + 16, true);
      const coords = Array.from({length: 4}, (_, j) => view.getFloat32(offset + 20 + 4 * j, true));
      if (coords.some(v => !Number.isFinite(v) || v < 0 || v > 1)) throw new Error("Invalid coordinate");
      segments.push({id: key(clientID, sequence), clientID, sequence,
        x1: coords[0], y1: coords[1], x2: coords[2], y2: coords[3], at: snapshot ? time(offset + 36) : now});
    }
    return {type: snapshot ? "snapshot" : "draw", now, lifetime, segments};
  }
  return {key, encodeDraw, decode};
})();
