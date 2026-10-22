package main

import (
	"encoding/json"
	"testing"
	"time"
)

func testPeer() *peer {
	return &peer{out: make(chan []byte, 64), done: make(chan struct{})}
}

func receive(t *testing.T, p *peer) message {
	t.Helper()
	select {
	case data := <-p.out:
		var msg message
		if err := json.Unmarshal(data, &msg); err != nil {
			t.Fatal(err)
		}
		return msg
	default:
		t.Fatal("missing message")
		return message{}
	}
}

func TestSharedDrawingAndSnapshot(t *testing.T) {
	r := newRoom()
	a, b := testPeer(), testPeer()
	for _, p := range []*peer{a, b} {
		r.join(p)
		msg := receive(t, p)
		if msg.Type != "snapshot" || len(msg.Segments) != 0 || msg.Lifetime != 10000 {
			t.Fatalf("unexpected initial snapshot: %+v", msg)
		}
	}
	if !r.draw([]byte(`{"type":"draw","segments":[{"id":"a:1","x1":0.1,"y1":0.2,"x2":0.3,"y2":0.4,"at":1}]}`)) {
		t.Fatal("valid drawing rejected")
	}
	for _, p := range []*peer{a, b} {
		msg := receive(t, p)
		if msg.Type != "draw" || len(msg.Segments) != 1 || msg.Segments[0].ID != "a:1" || msg.Segments[0].At <= 1 {
			t.Fatalf("drawing not echoed/broadcast with server timestamp: %+v", msg)
		}
	}
	c := testPeer()
	r.join(c)
	if msg := receive(t, c); len(msg.Segments) != 1 || msg.Segments[0].X2 != 0.3 {
		t.Fatalf("late join lost drawing: %+v", msg)
	}
	r.mu.Lock()
	r.segments[0].At = time.Now().Add(-lifetime).UnixMilli()
	r.mu.Unlock()
	d := testPeer()
	r.join(d)
	if msg := receive(t, d); len(msg.Segments) != 0 {
		t.Fatal("expired drawing restored")
	}
}

func TestDrawingLimits(t *testing.T) {
	r := newRoom()
	for _, data := range []string{
		`{`, `{"type":"draw","segments":[]}`, `{"type":"other","segments":[{"id":"x"}]}`,
		`{"type":"draw","segments":[{"id":"x","x1":-0.1}]}`,
		`{"type":"draw","segments":[{"id":"x","y2":1.1}]}`,
		`{"type":"draw","segments":[{"id":""}]}`,
	} {
		if r.draw([]byte(data)) {
			t.Fatalf("accepted invalid message: %s", data)
		}
	}
	now := time.Now().UnixMilli()
	for range maxSegments {
		r.segments = append(r.segments, segment{ID: "old", At: now})
	}
	if !r.draw([]byte(`{"type":"draw","segments":[{"id":"new"}]}`)) || len(r.segments) != maxSegments || r.segments[maxSegments-1].ID != "new" {
		t.Fatal("memory cap did not preserve newest segment")
	}
	r.prune(now + lifetime.Milliseconds() + 1000)
	if len(r.segments) != 0 {
		t.Fatal("idle cleanup left expired segments")
	}
}
