package main

import (
	"bytes"
	"encoding/json"
	"io"
	"unicode/utf8"
)

const protocolVersion = 2
const maxOutput = 8 << 20

type request struct {
	V      int     `json:"v"`
	Type   string  `json:"type"`
	Board  string  `json:"board,omitempty"`
	Author string  `json:"author,omitempty"`
	View   int64   `json:"view,omitempty"`
	Area   *bounds `json:"area,omitempty"`
	Edit   *edit   `json:"edit,omitempty"`
}
type response struct {
	V        int       `json:"v"`
	Type     string    `json:"type"`
	Board    string    `json:"board,omitempty"`
	View     int64     `json:"view,omitempty"`
	Revision int64     `json:"revision"`
	Drawings []drawing `json:"drawings,omitempty"`
	History  []string  `json:"history"`
	ID       string    `json:"id,omitempty"`
	Code     string    `json:"code,omitempty"`
}

func decodeRequest(data []byte) (request, error) {
	var req request
	if len(data) > maxMessage || !utf8.Valid(data) {
		return req, errInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		return req, errInvalid
	}
	if decoder.Decode(new(any)) != io.EOF || req.V != protocolVersion {
		return req, errInvalid
	}
	switch req.Type {
	case "view":
		if !idPattern.MatchString(req.Author) || req.View < 1 || req.View > 1<<53-1 || req.Area == nil || !req.Area.valid(maxViewSpan) || req.Edit != nil {
			return req, errInvalid
		}
	case "edit":
		if req.Edit == nil || req.Author != "" || req.Area != nil || req.View != 0 {
			return req, errInvalid
		}
		if err := req.Edit.validate(); err != nil {
			return req, err
		}
	default:
		return req, errInvalid
	}
	if !idPattern.MatchString(req.Board) {
		return req, errInvalid
	}
	return req, nil
}

// encoding/json normally pads/truncates fixed arrays; coordinates require exact arity.
func (p *point) UnmarshalJSON(raw []byte) error {
	var values []*float64
	if err := json.Unmarshal(raw, &values); err != nil || len(values) != 2 {
		return errInvalid
	}
	for i, v := range values {
		if v == nil {
			return errInvalid
		}
		p[i] = *v
	}
	return nil
}
func (b *bounds) UnmarshalJSON(raw []byte) error {
	var values []*float64
	if err := json.Unmarshal(raw, &values); err != nil || len(values) != 4 {
		return errInvalid
	}
	for i, v := range values {
		if v == nil {
			return errInvalid
		}
		b[i] = *v
	}
	return nil
}
