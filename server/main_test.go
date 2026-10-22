package main

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"math"
	"os"
	"strings"
	"testing"
	"time"
)

func testPeer() *peer { return &peer{out: make(chan []byte, 64), done: make(chan struct{})} }
func receive(t *testing.T, p *peer) []byte {
	t.Helper()
	select {
	case b := <-p.out:
		return b
	default:
		t.Fatal("missing message")
		return nil
	}
}
func input(segments ...segment) []byte {
	b := encodeHeader(inputDraw, len(segments), 0, 0)
	for i, s := range segments {
		encodeSegment(b[headerSize+i*recordSize:], s, false)
	}
	return b
}
func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("../testdata/" + name + ".hex")
	if err != nil {
		t.Fatal(err)
	}
	out, err := hex.DecodeString(strings.TrimSpace(string(b)))
	if err != nil {
		t.Fatal(err)
	}
	return out
}
func TestCodecFixture(t *testing.T) {
	var id [16]byte
	for i := range id {
		id[i] = byte(i)
	}
	s := segment{ID: id, Sequence: 0x12345678, X1: 0.1, Y1: 0.25, X2: 0.5, Y2: 1, At: 0x010203040506}
	if !bytes.Equal(input(s), fixture(t, "input")) {
		t.Fatal("input layout")
	}
	got, ok := decodeDraw(fixture(t, "input"))
	if !ok || got[0].ID != s.ID || got[0].X1 != s.X1 || got[0].Sequence != s.Sequence {
		t.Fatal("decode layout")
	}
	for _, kind := range []byte{serverDraw, snapshot} {
		life := uint32(0)
		name := "draw"
		if kind == snapshot {
			life = 10000
			name = "snapshot"
		}
		b := encodeHeader(kind, 1, s.At, life)
		encodeSegment(b[16:], s, kind == snapshot)
		if !bytes.Equal(b, fixture(t, name)) {
			t.Fatal(name + " layout")
		}
	}
}
func TestSharedDrawingAndSnapshot(t *testing.T) {
	r := newRoom()
	a, b := testPeer(), testPeer()
	for _, p := range []*peer{a, b} {
		r.join(p)
		data := receive(t, p)
		if len(data) != 16 || data[1] != snapshot || binary.LittleEndian.Uint32(data[12:]) != 10000 {
			t.Fatal("initial snapshot")
		}
	}
	s := segment{Sequence: 1, X2: 0.3}
	if !r.draw(input(s)) {
		t.Fatal("draw rejected")
	}
	first, second := receive(t, a), receive(t, b)
	if !bytes.Equal(first, second) || &first[0] != &second[0] || first[1] != serverDraw || binary.LittleEndian.Uint64(first[4:]) <= 1 {
		t.Fatal("shared timestamped output")
	}
	c := testPeer()
	r.join(c)
	data := receive(t, c)
	if len(data) != 60 || math.Float32frombits(binary.LittleEndian.Uint32(data[44:])) != s.X2 || binary.LittleEndian.Uint64(data[52:]) != binary.LittleEndian.Uint64(first[4:]) {
		t.Fatal("late snapshot")
	}
	r.segments[r.head].At = time.Now().Add(-lifetime).UnixMilli()
	d := testPeer()
	r.join(d)
	if len(receive(t, d)) != 16 {
		t.Fatal("expired snapshot")
	}
}
func TestDrawingLimits(t *testing.T) {
	valid := input(segment{})
	mutations := []func([]byte){func(b []byte) { b[0] = 2 }, func(b []byte) { b[1] = 2 }, func(b []byte) { b[2] = 0 }, func(b []byte) { b[2] = 129 }, func(b []byte) { b[4] = 1 }, func(b []byte) { b[12] = 1 }}
	for _, mutate := range mutations {
		b := bytes.Clone(valid)
		mutate(b)
		if newRoom().draw(b) {
			t.Fatal("accepted invalid header")
		}
	}
	for _, b := range [][]byte{nil, valid[:15], valid[:51], append(bytes.Clone(valid), 0), input(make([]segment, 129)...)} {
		if newRoom().draw(b) {
			t.Fatal("invalid length/count")
		}
	}
	for _, v := range []float32{float32(math.NaN()), float32(math.Inf(1)), float32(math.Inf(-1)), -0.1, 1.1} {
		for offset := 36; offset < 52; offset += 4 {
			b := bytes.Clone(valid)
			binary.LittleEndian.PutUint32(b[offset:], math.Float32bits(v))
			if newRoom().draw(b) {
				t.Fatal("invalid coordinate")
			}
		}
	}
	if !newRoom().draw(input(make([]segment, 128)...)) {
		t.Fatal("maximum batch rejected")
	}
}
func TestRing(t *testing.T) {
	r := newRoom()
	now := time.Now().UnixMilli()
	for i := 0; i < maxSegments+7; i++ {
		r.append(segment{Sequence: uint32(i), At: now})
	}
	if r.count != maxSegments || r.head != 7 {
		t.Fatal("ring overflow")
	}
	p := testPeer()
	r.join(p)
	b := receive(t, p)
	if len(b) != 16+44*maxSegments {
		t.Fatal("maximum snapshot size")
	}
	for i := 0; i < maxSegments; i++ {
		if binary.LittleEndian.Uint32(b[16+i*44+16:]) != uint32(i+7) {
			t.Fatal("snapshot order")
		}
	}
	for i := 0; i < 3; i++ {
		r.segments[(r.head+i)%maxSegments].At = now - 10000
	}
	r.prune(now)
	if r.count != maxSegments-3 || r.head != 10 {
		t.Fatal("partial expiration")
	}
	r.prune(now + 10000)
	if r.count != 0 {
		t.Fatal("full expiration")
	}
	for _, s := range r.segments {
		if s != (segment{}) {
			t.Fatal("expired slot retained")
		}
	}
	r.append(segment{Sequence: 99, At: now + 10000})
	if r.count != 1 || r.segments[r.head].Sequence != 99 {
		t.Fatal("reuse")
	}
}
func TestNondecreasingReceiptTime(t *testing.T) {
	r := newRoom()
	r.lastAt = time.Now().UnixMilli() + 10000
	if !r.draw(input(segment{})) || r.segments[r.head].At != r.lastAt {
		t.Fatal("timestamp decreased")
	}
}
