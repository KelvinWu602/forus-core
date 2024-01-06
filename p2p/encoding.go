package p2p

import (
	"bytes"
	"encoding/gob"
	"io"
)

type Codec interface {
	Decode(io.Reader, any) error
	Encode(any) *bytes.Buffer
}

// use gob to decode control message
type GOBCodec struct {
}

// r is the source
// v is the type the source should be decoded to
func (g GOBCodec) Decode(r io.Reader, v any) error {
	return gob.NewDecoder(r).Decode(v)
}

func (g GOBCodec) Encode(v any) *bytes.Buffer {
	buf := new(bytes.Buffer)
	gob.NewEncoder(buf).Encode(v)
	return buf
}
