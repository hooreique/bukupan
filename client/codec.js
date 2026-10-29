"use strict";

// v2 uses UTF-8 JSON in binary frames. Doubles preserve distant world coordinates;
// the bounded schema keeps storage, retries and protocol changes inspectable.
const BoardCodec = (() => {
  const VERSION = 2, MAX_LINES = 40000, MAX_DRAWINGS = 4000, MAX_OUTPUT = 8 * 1024 * 1024;
  const id = value => typeof value === "string" && /^[0-9a-f]{32}$/.test(value);
  const integer = value => Number.isSafeInteger(value) && value >= 0;
  const encoder = new TextEncoder(), decoder = new TextDecoder("utf-8", { fatal: true });
  function encode(message) {
    const bytes = encoder.encode(JSON.stringify({ ...message, v: VERSION }));
    if (bytes.length > 32768) throw new Error("Operation is too large");
    return bytes;
  }
  function decode(buffer) {
    if (!(buffer instanceof ArrayBuffer) || buffer.byteLength > MAX_OUTPUT) throw new Error("Invalid frame");
    const message = JSON.parse(decoder.decode(buffer));
    if (!message || message.v !== VERSION || !integer(message.revision)) throw new Error("Incompatible server");
    if (message.history != null && (!Array.isArray(message.history) || message.history.length > 50 || !message.history.every(id))) throw new Error("Invalid history");
    switch (message.type) {
      case "hello": if (!id(message.board)) throw new Error("Invalid board"); break;
      case "snapshot": case "patch": {
        if (!integer(message.view) || !message.view) throw new Error("Invalid view");
        message.drawings ??= [];
        // Undo can remove an entire gesture; empty records still have a fixed bound.
        if (!Array.isArray(message.drawings) || message.drawings.length > MAX_DRAWINGS) throw new Error("Invalid drawings");
        let count = 0;
        for (const d of message.drawings) {
          if (!id(d.id) || !Number.isFinite(d.width) || d.width < .25 || d.width > 256 || !Array.isArray(d.lines)) throw new Error("Invalid stroke");
          count += d.lines.length;
          if (count > MAX_LINES) throw new Error("Too many lines");
          for (const line of d.lines) {
            if (!Array.isArray(line) || line.length !== 4 || !line.every(v => Number.isFinite(v) && Math.abs(v) <= 1e9)) throw new Error("Invalid coordinates");
          }
        }
        break;
      }
      case "ack": if (!id(message.id)) throw new Error("Invalid receipt"); break;
      case "error": if (!["storage", "dense", "invalid", "board"].includes(message.code)) throw new Error("Invalid error"); break;
      default: throw new Error("Unknown message");
    }
    return message;
  }
  function randomID() {
    return Array.from(crypto.getRandomValues(new Uint8Array(16)), b => b.toString(16).padStart(2, "0")).join("");
  }
  return { encode, decode, randomID, id, MAX_LINES, MAX_DRAWINGS };
})();
if (typeof module !== "undefined") module.exports = BoardCodec;
