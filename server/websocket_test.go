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
		ws := &websocket{reader: bufio.NewReader(bytes.NewReader(maskedFrame(opText, true, payload)))}
		op, final, got, err := ws.readFrame()
		if err != nil || op != opText || !final || !bytes.Equal(got, payload) {
			t.Fatalf("frame length %d: %v", size, err)
		}
	}
	for _, frame := range [][]byte{
		{0x81, 0},    // unmasked
		{0xc1, 0x80}, // unnegotiated extension
		{0x81, 0xff}, // too large
		maskedFrame(opPing, false, nil),
		maskedFrame(opPing, true, bytes.Repeat([]byte("x"), 126)),
		maskedFrame(opText, true, bytes.Repeat([]byte("x"), maxMessage+1)),
	} {
		ws := &websocket{reader: bufio.NewReader(bytes.NewReader(frame))}
		if _, _, _, err := ws.readFrame(); err == nil {
			t.Fatalf("accepted invalid frame: %x", frame[:2])
		}
	}
}

func TestFragmentedTextWithPing(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	client.SetDeadline(time.Now().Add(2 * time.Second))
	ws := &websocket{conn: server, reader: bufio.NewReader(server)}
	result := make(chan error, 1)
	go func() {
		data, err := ws.readMessage()
		if err == nil && string(data) != "hello" {
			err = io.ErrUnexpectedEOF
		}
		result <- err
	}()
	for _, frame := range [][]byte{
		maskedFrame(opText, false, []byte("hel")),
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
