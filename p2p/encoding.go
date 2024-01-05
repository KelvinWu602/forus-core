package p2p

import (
	"encoding/gob"
	"io"
)

type Decoder interface {
	Decode(io.Reader, any) error
}

// use gob to decode control message
type GOBDecoder struct {
}

// r is the source
// v is the type the source should be decoded to
func (g GOBDecoder) Decode(r io.Reader, v any) error {
	return gob.NewDecoder(r).Decode(v)
}
