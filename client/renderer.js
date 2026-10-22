"use strict";

// Capsules share a maximum-intensity surface: overlapping caps never add light.
function createStrokeRenderer(canvas, capacity) {
  const gl = canvas.getContext("webgl2", { alpha: false, antialias: false, depth: false, stencil: false });
  if (!gl) return null;
  let origin = Date.now();
  const data = new Float32Array(capacity * 5);
  let program, buffer, viewport, time, duration;
  let dirty = true;
  let count = 0;
  function shader(type, source) {
    const result = gl.createShader(type);
    gl.shaderSource(result, source);
    gl.compileShader(result);
    if (!gl.getShaderParameter(result, gl.COMPILE_STATUS)) {
      throw new Error(gl.getShaderInfoLog(result));
    }
    return result;
  }
  function initialize() {
    const vertex = shader(gl.VERTEX_SHADER, `#version 300 es
      precision highp float;
      layout(location=0) in vec4 endpoints;
      layout(location=1) in float created;
      uniform vec2 viewport;
      uniform float time, duration;
      out vec2 local;
      flat out float segmentLength, brightness;
      void main() {
        float size = max(viewport.x, viewport.y);
        vec2 a = endpoints.xy * size + (viewport - size) * 0.5;
        vec2 b = endpoints.zw * size + (viewport - size) * 0.5;
        vec2 delta = b - a;
        segmentLength = length(delta);
        vec2 tangent = segmentLength > 0.0 ? delta / segmentLength : vec2(1.0, 0.0);
        vec2 normal = vec2(-tangent.y, tangent.x);
        vec2 corner = vec2(float(gl_VertexID & 1), float(gl_VertexID >> 1));
        local = vec2(mix(-2.5, segmentLength + 2.5, corner.x), mix(-2.5, 2.5, corner.y));
        vec2 p = a + tangent * local.x + normal * local.y;
        gl_Position = vec4(p / viewport * vec2(2.0, -2.0) + vec2(-1.0, 1.0), 0.0, 1.0);
        brightness = 1.0 - smoothstep(0.0, duration, max(0.0, time - created));
      }`);
    const fragment = shader(gl.FRAGMENT_SHADER, `#version 300 es
      precision highp float;
      in vec2 local;
      flat in float segmentLength, brightness;
      out vec4 color;
      void main() {
        vec2 nearest = vec2(clamp(local.x, 0.0, segmentLength), 0.0);
        float distance = length(local - nearest);
        float aa = max(fwidth(distance), 0.001);
        float coverage = 1.0 - smoothstep(1.5 - aa * 0.5, 1.5 + aa * 0.5, distance);
        color = vec4(vec3(coverage * brightness), 1.0);
      }`);
    program = gl.createProgram();
    gl.attachShader(program, vertex);
    gl.attachShader(program, fragment);
    gl.linkProgram(program);
    gl.deleteShader(vertex);
    gl.deleteShader(fragment);
    if (!gl.getProgramParameter(program, gl.LINK_STATUS)) throw new Error(gl.getProgramInfoLog(program));
    gl.useProgram(program);
    buffer = gl.createBuffer();
    gl.bindBuffer(gl.ARRAY_BUFFER, buffer);
    gl.bufferData(gl.ARRAY_BUFFER, data.byteLength, gl.DYNAMIC_DRAW);
    for (const [index, size, offset] of [[0, 4, 0], [1, 1, 16]]) {
      gl.enableVertexAttribArray(index);
      gl.vertexAttribPointer(index, size, gl.FLOAT, false, 20, offset);
      gl.vertexAttribDivisor(index, 1);
    }
    viewport = gl.getUniformLocation(program, "viewport");
    time = gl.getUniformLocation(program, "time");
    duration = gl.getUniformLocation(program, "duration");
    gl.enable(gl.BLEND);
    gl.blendEquation(gl.MAX);
    gl.clearColor(0, 0, 0, 1);
    dirty = true;
  }
  initialize();
  canvas.addEventListener("webglcontextlost", (event) => event.preventDefault());
  canvas.addEventListener("webglcontextrestored", () => { initialize(); requestDraw(); });
  return {
    invalidate() { dirty = true; },
    render(segments, timestamp, lifetime, width, height) {
      if (gl.isContextLost()) return;
      if (dirty) {
        // Keep float timestamps precise even in a tab left open for days.
        origin = timestamp;
        count = 0;
        for (const segment of segments.values()) {
          const offset = count++ * 5;
          data[offset] = segment.x1;
          data[offset + 1] = segment.y1;
          data[offset + 2] = segment.x2;
          data[offset + 3] = segment.y2;
          data[offset + 4] = segment.at - origin;
        }
        gl.bindBuffer(gl.ARRAY_BUFFER, buffer);
        if (count) gl.bufferSubData(gl.ARRAY_BUFFER, 0, data.subarray(0, count * 5));
        dirty = false;
      }
      gl.viewport(0, 0, canvas.width, canvas.height);
      gl.clear(gl.COLOR_BUFFER_BIT);
      gl.uniform2f(viewport, width, height);
      gl.uniform1f(time, timestamp - origin);
      gl.uniform1f(duration, lifetime);
      gl.drawArraysInstanced(gl.TRIANGLE_STRIP, 0, 4, count);
    },
  };
}
