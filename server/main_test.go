package main

import (
	"encoding/json"
	"os"
	"testing"
)

func TestProtocolSharedFixture(t *testing.T) {
	raw, err := os.ReadFile("../testdata/protocol-v2.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Request  json.RawMessage `json:"request"`
		Response json.RawMessage `json:"response"`
	}
	if err = json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	req, err := decodeRequest(fixture.Request)
	if err != nil {
		t.Fatal(err)
	}
	if req.Edit.Points[1][1] != 100000001.125 {
		t.Fatal("lost double precision")
	}
	var output response
	if err = json.Unmarshal(fixture.Response, &output); err != nil {
		t.Fatal(err)
	}
	if output.Drawings[0].Lines[0][3] != req.Edit.Points[1][1] {
		t.Fatal("fixture geometry differs")
	}
	for _, raw := range []string{`[1]`, `[1,2,3]`, `[null,1]`, `[1,"2"]`} {
		var p point
		if json.Unmarshal([]byte(raw), &p) == nil {
			t.Fatal("invalid point accepted", raw)
		}
	}
}

func testPeer() *peer { return &peer{out: make(chan []byte, 64), done: make(chan struct{})} }
func receive(t *testing.T, p *peer) response {
	t.Helper()
	select {
	case b := <-p.out:
		p.queued.Add(-int64(len(b)))
		var message response
		if err := json.Unmarshal(b, &message); err != nil {
			t.Fatal(err)
		}
		return message
	default:
		t.Fatal("missing message")
		return response{}
	}
}
func send(t *testing.T, r *room, p *peer, req request) {
	t.Helper()
	req.V = protocolVersion
	req.Board = r.store.ID
	data, _ := json.Marshal(req)
	if !r.handle(p, data) {
		t.Fatal("request rejected")
	}
}
func joinView(t *testing.T, r *room, p *peer, author string, area bounds) {
	t.Helper()
	r.join(p)
	if receive(t, p).Type != "hello" {
		t.Fatal("hello missing")
	}
	send(t, r, p, request{Type: "view", Author: author, View: 1, Area: &area})
	if receive(t, p).Type != "snapshot" {
		t.Fatal("snapshot missing")
	}
}
func TestDurableSharingRetryAndViewport(t *testing.T) {
	r := newRoom(testBoard(t))
	a, b := testPeer(), testPeer()
	author := randomID()
	joinView(t, r, a, author, bounds{-100, -100, 100, 100})
	joinView(t, r, b, randomID(), bounds{10000, 10000, 10100, 10100})
	op := pen(author, point{0, 0}, point{10, 10})
	send(t, r, a, request{Type: "edit", Edit: &op})
	patch := receive(t, a)
	if patch.Type != "patch" || len(patch.Drawings) != 1 {
		t.Fatal("local patch missing")
	}
	if got := receive(t, b); len(got.Drawings) != 1 || len(got.Drawings[0].Lines) != 0 {
		t.Fatal("outside geometry leaked")
	}
	ack := receive(t, a)
	if ack.Type != "ack" || ack.Revision != 1 || len(boardView(t, r.store, bounds{-10, -10, 20, 20})) != 1 {
		t.Fatal("ACK before durable edit")
	}
	send(t, r, a, request{Type: "edit", Edit: &op})
	if got := receive(t, a); got.Type != "ack" || got.Revision != 1 {
		t.Fatal("retry duplicated edit")
	}
	c := testPeer()
	joinView(t, r, c, randomID(), bounds{-100, -100, 100, 100})
}
func TestStorageFailureIsNotAcknowledged(t *testing.T) {
	r := newRoom(testBoard(t))
	p := testPeer()
	author := randomID()
	joinView(t, r, p, author, bounds{-100, -100, 100, 100})
	if _, err := r.store.db.Exec("PRAGMA query_only=ON"); err != nil {
		t.Fatal(err)
	}
	op := pen(author, point{0, 0})
	send(t, r, p, request{Type: "edit", Edit: &op})
	if got := receive(t, p); got.Type != "error" || got.Code != "storage" || got.ID != op.ID {
		t.Fatal("failed storage was acknowledged")
	}
}
func TestProtocolValidationAndBoardIdentity(t *testing.T) {
	r := newRoom(testBoard(t))
	p := testPeer()
	author := randomID()
	joinView(t, r, p, author, bounds{-100, -100, 100, 100})
	for _, data := range []string{`{}`, `{"v":1,"type":"view"}`, `{"v":2,"type":"unknown"}`, `{"v":2,"type":"view","unexpected":1}`, `{"v":2,"area":[1,2]}`, `{"v":2,"edit":{"points":[[1,2,3]]}}`} {
		if r.handle(p, []byte(data)) {
			t.Fatal("invalid input accepted", data)
		}
	}
	area := bounds{-10, -10, 10, 10}
	raw, _ := json.Marshal(request{V: 2, Type: "view", Board: randomID(), Author: author, View: 2, Area: &area})
	if !r.handle(p, raw) || receive(t, p).Code != "board" {
		t.Fatal("board mismatch not reported")
	}
}
func TestSlowPeerIsDisconnected(t *testing.T) {
	p := &peer{out: make(chan []byte, 1), done: make(chan struct{})}
	p.enqueue(response{Type: "hello"})
	p.enqueue(response{Type: "hello"})
	select {
	case <-p.done:
	default:
		t.Fatal("slow peer retained unbounded queue")
	}
}
