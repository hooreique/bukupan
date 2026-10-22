// Run with node scripts/test-codec.js; uses the same fixtures as Go.
const fs = require("node:fs");
const vm = require("node:vm");
const assert = require("node:assert/strict");
vm.runInThisContext(fs.readFileSync("client/codec.js", "utf8"));
const fixture = name => Uint8Array.from(Buffer.from(fs.readFileSync(`testdata/${name}.hex`, "utf8").trim(), "hex")).buffer;
const s = {clientID: Uint8Array.from({length:16}, (_,i)=>i), sequence:0x12345678, x1:Math.fround(0.1), y1:0.25, x2:0.5, y2:1};
assert.deepEqual(strokeCodec.encodeDraw([s]), fixture("input"));
for (const name of ["draw","snapshot"]) {
  const m=strokeCodec.decode(fixture(name));
  assert.equal(m.now,0x010203040506);
  assert.equal(m.segments[0].at,m.now);
  assert.equal(m.segments[0].id,strokeCodec.key(s.clientID,s.sequence));
  for (const k of ["x1","y1","x2","y2","sequence"]) assert.equal(m.segments[0][k],s[k]);
}
function rejects(mutate, name="draw") {const b=fixture(name);mutate(new DataView(b));assert.throws(()=>strokeCodec.decode(b));}
for (const [offset,value] of [[0,2],[1,1],[1,4],[2,0],[2,129],[12,1]]) rejects(v=>v.setUint8(offset,value));
rejects(v=>v.setUint16(2,16001,true),"snapshot");
rejects(v=>v.setUint32(12,0,true),"snapshot");
rejects(v=>v.setBigUint64(4,2n**63n,true));
for (const v of [NaN,Infinity,-Infinity,-0.1,1.1]) for (let offset=36;offset<52;offset+=4) rejects(d=>d.setFloat32(offset,v,true));
for (const b of ["text",new ArrayBuffer(0),fixture("draw").slice(0,51),new ArrayBuffer(53)]) assert.throws(()=>strokeCodec.decode(b));
for (const n of [0,16000]) {
 const b=new ArrayBuffer(16+n*44),v=new DataView(b);
 v.setUint8(0,1);v.setUint8(1,3);v.setUint16(2,n,true);v.setUint32(12,10000,true);
 assert.equal(strokeCodec.decode(b).segments.length,n);
}
assert.throws(()=>strokeCodec.encodeDraw([]));
assert.throws(()=>strokeCodec.encodeDraw(Array(129).fill(s)));
console.log("PASS codec fixtures, validation, empty and maximum snapshots");
