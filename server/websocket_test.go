package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"
)

func maskedFrame(op byte, final bool, payload []byte) []byte {
	header := op
	if final {
		header |= 0x80
	}
	frame := []byte{header}
	if len(payload) < 126 {
		frame = append(frame, 0x80|byte(len(payload)))
	} else {
		frame = binary.BigEndian.AppendUint16(append(frame, 0xfe), uint16(len(payload)))
	}
	mask := []byte{1, 2, 3, 4}
	frame = append(frame, mask...)
	for i, b := range payload {
		frame = append(frame, b^mask[i%4])
	}
	return frame
}

func TestClientFrames(t *testing.T) {
	for _, size := range []int{0, 125, 126, maxMessage} {
		payload := bytes.Repeat([]byte("x"), size)
		ws := &websocket{reader: bufio.NewReader(bytes.NewReader(maskedFrame(opBinary, true, payload)))}
		op, final, got, err := ws.readFrame()
		if err != nil || op != opBinary || !final || !bytes.Equal(got, payload) {
			t.Fatalf("frame length %d: %v", size, err)
		}
	}
	for _, frame := range [][]byte{
		{0x81, 0},    // unmasked
		{0xc1, 0x80}, // unnegotiated extension
		{0x81, 0xff}, // too large
		maskedFrame(opPing, false, nil),
		maskedFrame(opPing, true, bytes.Repeat([]byte("x"), 126)),
		maskedFrame(opBinary, true, bytes.Repeat([]byte("x"), maxMessage+1)),
	} {
		ws := &websocket{reader: bufio.NewReader(bytes.NewReader(frame))}
		if _, _, _, err := ws.readFrame(); err == nil {
			t.Fatalf("accepted invalid frame: %x", frame[:2])
		}
	}
}

func TestFragmentedBinaryWithPing(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	client.SetDeadline(time.Now().Add(2 * time.Second))
	ws := &websocket{conn: server, reader: bufio.NewReader(server)}
	result := make(chan error, 1)
	go func() {
		data, err := ws.readMessage()
		if err == nil && string(data) != "\xffello" {
			err = io.ErrUnexpectedEOF
		}
		result <- err
	}()
	for _, frame := range [][]byte{
		maskedFrame(opBinary, false, []byte("\xffel")),
		maskedFrame(opPing, true, []byte("ping")),
	} {
		if _, err := client.Write(frame); err != nil {
			t.Fatal(err)
		}
	}
	var pong [6]byte
	if _, err := io.ReadFull(client, pong[:]); err != nil || !bytes.Equal(pong[:], []byte{0x8a, 4, 'p', 'i', 'n', 'g'}) {
		t.Fatalf("missing pong: %x (%v)", pong, err)
	}
	if _, err := client.Write(maskedFrame(opContinuation, true, []byte("lo"))); err != nil {
		t.Fatal(err)
	}
	if err := <-result; err != nil {
		t.Fatal(err)
	}
}

func TestRejectedMessages(t *testing.T) {
	for _, frames := range [][][]byte{
		{maskedFrame(opText, true, []byte("text"))},
		{maskedFrame(opContinuation, true, nil)},
		{maskedFrame(opBinary, false, nil), maskedFrame(opBinary, true, nil)},
		{maskedFrame(opBinary, false, bytes.Repeat([]byte{0xff}, maxMessage)), maskedFrame(opContinuation, true, []byte{0})},
		{maskedFrame(opClose, true, []byte{0x03, 0xe8, 0xff})},
	} {
		server, client := net.Pipe()
		ws := &websocket{conn: server, reader: bufio.NewReader(bytes.NewReader(bytes.Join(frames, nil)))}
		if _, err := ws.readMessage(); err == nil {
			t.Fatal("accepted invalid message")
		}
		server.Close()
		client.Close()
	}
}

func TestLargeOutputSnapshot(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	client.SetDeadline(time.Now().Add(2 * time.Second))
	ws := &websocket{conn: server}
	payload := make([]byte, headerSize+maxSegments*snapshotRecordSize)
	done := make(chan error, 1)
	go func() { done <- ws.writeFrame(opBinary, payload) }()
	var header [10]byte
	if _, err := io.ReadFull(client, header[:]); err != nil {
		t.Fatal(err)
	}
	if header[0] != 0x82 || header[1] != 127 || binary.BigEndian.Uint64(header[2:]) != uint64(len(payload)) {
		t.Fatal("large frame header")
	}
	got := make([]byte, len(payload))
	if _, err := io.ReadFull(client, got); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
