const { test } = require('node:test');
const assert = require('node:assert/strict');
const codec = require('../../client/codec.js');
const fixture = require('../../testdata/protocol-v2.json');
const frame = value => new TextEncoder().encode(JSON.stringify(value)).buffer;

test('v2 shared fixture preserves double coordinates and operation identity', () => {
  assert.deepEqual(JSON.parse(new TextDecoder().decode(codec.encode(fixture.request))), fixture.request);
  assert.deepEqual(codec.decode(frame(fixture.response)), fixture.response);
});
test('reject unsupported versions, malformed geometry and unbounded messages', () => {
  for (const change of [
    m => {m.v=1;}, m => {m.type='unknown';}, m => {m.revision=1e20;}, m => {m.view=-1;},
    m => {m.drawings[0].lines=[[0,0,1]];}, m => {m.drawings[0].lines=[[0,0,null,1]];},
    m => {m.drawings[0].lines=[[0,0,1e10,1]];}, m => {m.drawings[0].width=-1;},
    m => {m.drawings[0].id='bad';}, m => {m.history=['bad'];},
    m => {m.drawings[0].lines=Array(40001).fill([0,0,0,0]);},
  ]) { const m=structuredClone(fixture.response);change(m);assert.throws(()=>codec.decode(frame(m))); }
  assert.throws(()=>codec.decode(new Uint8Array([255]).buffer));
  assert.throws(()=>codec.decode(new ArrayBuffer(8*1024*1024+1)));
  assert.throws(()=>codec.encode({ huge:'x'.repeat(32768) }));
});
test('empty snapshot and maximum visible geometry are accepted', () => {
  const empty=codec.decode(frame({v:2,type:'snapshot',view:1,revision:0,history:[]}));
  assert.deepEqual(empty.drawings,[]);
  const large=structuredClone(fixture.response);large.drawings[0].lines=Array(40000).fill([0,0,1,1]);
  assert.equal(codec.decode(frame(large)).drawings[0].lines.length,40000);
});
