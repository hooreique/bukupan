package main

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	opContinuation = 0
	opText         = 1
	opClose        = 8
	opPing         = 9
	opPong         = 10
	maxMessage     = 32 * 1024
)

// A small RFC 6455 transport: masked client frames, text fragmentation, and
// control frames. No compression or extensions are negotiated.
type websocket struct {
	conn    net.Conn
	reader  *bufio.Reader
	writeMu sync.Mutex
}

func hasToken(value, token string) bool {
	for _, part := range strings.Split(value, ",") {
		if strings.EqualFold(strings.TrimSpace(part), token) {
			return true
		}
	}
	return false
}

func upgrade(w http.ResponseWriter, r *http.Request) (*websocket, error) {
	key := r.Header.Get("Sec-WebSocket-Key")
	decoded, err := base64.StdEncoding.DecodeString(key)
	if r.Method != http.MethodGet || !hasToken(r.Header.Get("Connection"), "upgrade") ||
		!hasToken(r.Header.Get("Upgrade"), "websocket") || r.Header.Get("Sec-WebSocket-Version") != "13" ||
		err != nil || len(decoded) != 16 {
		w.Header().Set("Sec-WebSocket-Version", "13")
		http.Error(w, "WebSocket upgrade required", http.StatusBadRequest)
		return nil, errors.New("invalid WebSocket handshake")
	}
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "WebSocket unavailable", http.StatusInternalServerError)
		return nil, errors.New("hijacking unavailable")
	}
	conn, rw, err := hijacker.Hijack()
	if err != nil {
		return nil, err
	}
	// SHA-1 is required by the WebSocket handshake; this is not authentication.
	sum := sha1.Sum([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	fmt.Fprintf(rw, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\n\r\n", base64.StdEncoding.EncodeToString(sum[:]))
	if err := rw.Flush(); err != nil {
		conn.Close()
		return nil, err
	}
	return &websocket{conn: conn, reader: rw.Reader}, nil
}

func (ws *websocket) writeFrame(op byte, payload []byte) error {
	ws.writeMu.Lock()
	defer ws.writeMu.Unlock()
	ws.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	header := []byte{0x80 | op}
	switch n := len(payload); {
	case n < 126:
		header = append(header, byte(n))
	case n <= 65535:
		header = binary.BigEndian.AppendUint16(append(header, 126), uint16(n))
	default:
		header = binary.BigEndian.AppendUint64(append(header, 127), uint64(n))
	}
	buffers := net.Buffers{header, payload}
	_, err := buffers.WriteTo(ws.conn)
	return err
}

func (ws *websocket) readFrame() (byte, bool, []byte, error) {
	var header [2]byte
	if _, err := io.ReadFull(ws.reader, header[:]); err != nil {
		return 0, false, nil, err
	}
	final, op := header[0]&0x80 != 0, header[0]&0x0f
	if header[0]&0x70 != 0 || header[1]&0x80 == 0 {
		return 0, false, nil, errors.New("reserved bits or unmasked client frame")
	}
	n := uint64(header[1] & 0x7f)
	if n == 126 {
		var size [2]byte
		if _, err := io.ReadFull(ws.reader, size[:]); err != nil {
			return 0, false, nil, err
		}
		n = uint64(binary.BigEndian.Uint16(size[:]))
		if n < 126 {
			return 0, false, nil, errors.New("non-minimal frame length")
		}
	} else if n == 127 {
		// All 64-bit payload lengths exceed our application limit.
		return 0, false, nil, errors.New("frame too large")
	}
	if n > maxMessage || (op >= opClose && (!final || n > 125)) {
		return 0, false, nil, errors.New("invalid frame length or fragmented control frame")
	}
	var mask [4]byte
	if _, err := io.ReadFull(ws.reader, mask[:]); err != nil {
		return 0, false, nil, err
	}
	payload := make([]byte, int(n))
	if _, err := io.ReadFull(ws.reader, payload); err != nil {
		return 0, false, nil, err
	}
	for i := range payload {
		payload[i] ^= mask[i%4]
	}
	return op, final, payload, nil
}

func (ws *websocket) readMessage() ([]byte, error) {
	var message []byte
	fragmented := false
	for {
		ws.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		op, final, data, err := ws.readFrame()
		if err != nil {
			return nil, err
		}
		switch op {
		case opPing:
			if err := ws.writeFrame(opPong, data); err != nil {
				return nil, err
			}
			continue
		case opPong:
			continue
		case opClose:
			if len(data) == 1 || (len(data) > 1 && !validClose(data)) {
				return nil, errors.New("invalid close frame")
			}
			ws.writeFrame(opClose, data)
			return nil, io.EOF
		case opText:
			if fragmented {
				return nil, errors.New("text frame during fragmented message")
			}
		case opContinuation:
			if !fragmented {
				return nil, errors.New("unexpected continuation")
			}
		default:
			return nil, errors.New("unsupported opcode")
		}
		if len(message)+len(data) > maxMessage {
			return nil, errors.New("message too large")
		}
		message = append(message, data...)
		if final {
			if !utf8.Valid(message) {
				return nil, errors.New("invalid UTF-8")
			}
			return message, nil
		}
		fragmented = true
	}
}

func validClose(data []byte) bool {
	code := binary.BigEndian.Uint16(data[:2])
	valid := code >= 3000 && code <= 4999 || code >= 1000 && code <= 1014 && code != 1004 && code != 1005 && code != 1006
	return valid && utf8.Valid(data[2:])
}
