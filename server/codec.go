package main

import (
	"encoding/binary"
	"math"
)

const (
	headerSize         = 16
	recordSize         = 36
	snapshotRecordSize = 44
	inputDraw          = 1
	serverDraw         = 2
	snapshot           = 3
)

type segment struct {
	ID             [16]byte
	Sequence       uint32
	X1, Y1, X2, Y2 float32
	At             int64
}

func decodeDraw(data []byte) ([]segment, bool) {
	if len(data) < headerSize || data[0] != 1 || data[1] != inputDraw {
		return nil, false
	}
	n := int(binary.LittleEndian.Uint16(data[2:4]))
	if n < 1 || n > maxBatch || len(data) != headerSize+n*recordSize || binary.LittleEndian.Uint64(data[4:12]) != 0 || binary.LittleEndian.Uint32(data[12:16]) != 0 {
		return nil, false
	}
	segments := make([]segment, n)
	for i := range segments {
		b := data[headerSize+i*recordSize:]
		s := &segments[i]
		copy(s.ID[:], b[:16])
		s.Sequence = binary.LittleEndian.Uint32(b[16:20])
		coords := [4]*float32{&s.X1, &s.Y1, &s.X2, &s.Y2}
		for j, p := range coords {
			v := math.Float32frombits(binary.LittleEndian.Uint32(b[20+j*4:]))
			if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) || v < 0 || v > 1 {
				return nil, false
			}
			*p = v
		}
	}
	return segments, true
}

func encodeHeader(kind byte, n int, now int64, life uint32) []byte {
	size := recordSize
	if kind == snapshot {
		size = snapshotRecordSize
	}
	b := make([]byte, headerSize+n*size)
	b[0], b[1] = 1, kind
	binary.LittleEndian.PutUint16(b[2:4], uint16(n))
	binary.LittleEndian.PutUint64(b[4:12], uint64(now))
	binary.LittleEndian.PutUint32(b[12:16], life)
	return b
}

func encodeSegment(b []byte, s segment, withTime bool) {
	copy(b[:16], s.ID[:])
	binary.LittleEndian.PutUint32(b[16:20], s.Sequence)
	for j, v := range [4]float32{s.X1, s.Y1, s.X2, s.Y2} {
		binary.LittleEndian.PutUint32(b[20+j*4:], math.Float32bits(v))
	}
	if withTime {
		binary.LittleEndian.PutUint64(b[36:44], uint64(s.At))
	}
}
