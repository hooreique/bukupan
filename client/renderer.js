"use strict";

// Camera-relative capsules share a maximum-intensity surface: overlapping caps
// never add light. No timestamp/fade and no work while the scene is unchanged.
function createStrokeRenderer(canvas, capacity, onRestore = () => {}) {
  const gl = canvas.getContext("webgl2", { alpha: false, antialias: false, depth: false, stencil: false });
  if (!gl) return null;
  const data = new Float32Array(capacity * 5);
  let program, buffer, viewport;
  function shader(type, source) {
    const result = gl.createShader(type);
    gl.shaderSource(result, source); gl.compileShader(result);
    if (!gl.getShaderParameter(result, gl.COMPILE_STATUS)) throw new Error(gl.getShaderInfoLog(result));
    return result;
  }
  function initialize() {
    const vertex = shader(gl.VERTEX_SHADER, `#version 300 es
      precision highp float;
      layout(location=0) in vec4 endpoints;
      layout(location=1) in float width;
      uniform vec2 viewport;
      out vec2 local;
      flat out float segmentLength, radius;
      void main() {
        vec2 a = endpoints.xy, b = endpoints.zw;
        vec2 delta = b - a;
        segmentLength = length(delta);
        radius = width * 0.5;
        vec2 tangent = segmentLength > 0.0 ? delta / segmentLength : vec2(1.0, 0.0);
        vec2 normal = vec2(-tangent.y, tangent.x);
        vec2 corner = vec2(float(gl_VertexID & 1), float(gl_VertexID >> 1));
        float padding = radius + 1.0;
        local = vec2(mix(-padding, segmentLength + padding, corner.x), mix(-padding, padding, corner.y));
        vec2 p = a + tangent * local.x + normal * local.y;
        gl_Position = vec4(p / viewport * vec2(2.0, -2.0) + vec2(-1.0, 1.0), 0.0, 1.0);
      }`);
    const fragment = shader(gl.FRAGMENT_SHADER, `#version 300 es
      precision highp float;
      in vec2 local;
      flat in float segmentLength, radius;
      out vec4 color;
      void main() {
        vec2 nearest = vec2(clamp(local.x, 0.0, segmentLength), 0.0);
        float distance = length(local - nearest);
        float aa = max(fwidth(distance), 0.001);
        float coverage = 1.0 - smoothstep(radius - aa * 0.5, radius + aa * 0.5, distance);
        color = vec4(vec3(coverage), 1.0);
      }`);
    program = gl.createProgram(); gl.attachShader(program, vertex); gl.attachShader(program, fragment); gl.linkProgram(program);
    gl.deleteShader(vertex); gl.deleteShader(fragment);
    if (!gl.getProgramParameter(program, gl.LINK_STATUS)) throw new Error(gl.getProgramInfoLog(program));
    gl.useProgram(program);
    buffer = gl.createBuffer(); gl.bindBuffer(gl.ARRAY_BUFFER, buffer); gl.bufferData(gl.ARRAY_BUFFER, data.byteLength, gl.DYNAMIC_DRAW);
    for (const [index, size, offset] of [[0, 4, 0], [1, 1, 16]]) {
      gl.enableVertexAttribArray(index); gl.vertexAttribPointer(index, size, gl.FLOAT, false, 20, offset); gl.vertexAttribDivisor(index, 1);
    }
    viewport = gl.getUniformLocation(program, "viewport");
    gl.enable(gl.BLEND); gl.blendEquation(gl.MAX); gl.clearColor(0, 0, 0, 1);
  }
  initialize();
  canvas.addEventListener("webglcontextlost", event => event.preventDefault());
  canvas.addEventListener("webglcontextrestored", () => { initialize(); onRestore(); });
  return {
    render(drawings, camera, width, height) {
      if (gl.isContextLost()) return;
      let count = 0;
      for (const drawing of drawings) {
        for (const line of drawing.lines) {
          if (count >= capacity) throw new Error("Renderer capacity exceeded");
          const offset = count++ * 5;
          data[offset] = (line[0] - camera.x) * camera.zoom + width / 2;
          data[offset + 1] = (line[1] - camera.y) * camera.zoom + height / 2;
          data[offset + 2] = (line[2] - camera.x) * camera.zoom + width / 2;
          data[offset + 3] = (line[3] - camera.y) * camera.zoom + height / 2;
          data[offset + 4] = drawing.width * camera.zoom;
        }
      }
      gl.bindBuffer(gl.ARRAY_BUFFER, buffer);
      if (count) gl.bufferSubData(gl.ARRAY_BUFFER, 0, data.subarray(0, count * 5));
      gl.viewport(0, 0, canvas.width, canvas.height); gl.clear(gl.COLOR_BUFFER_BIT);
      gl.uniform2f(viewport, width, height);
      gl.drawArraysInstanced(gl.TRIANGLE_STRIP, 0, 4, count);
    },
  };
}
