// Package jsono builds JSON objects whose keys keep their insertion order.
//
// Go maps marshal with sorted keys. This study varies the order of questions
// and options inside request bodies, so every body is built from Obj values
// and the bytes on the wire follow the order the plan chose.
package jsono

import (
	"bytes"
	"encoding/json"
)

// KV is one key-value pair of an ordered object.
type KV struct {
	K string
	V any
}

// Obj is a JSON object that marshals its pairs in order.
type Obj []KV

// MarshalJSON writes the pairs in order, without HTML escaping.
func (o Obj) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, kv := range o {
		if i > 0 {
			b.WriteByte(',')
		}
		k, err := Marshal(kv.K)
		if err != nil {
			return nil, err
		}
		b.Write(k)
		b.WriteByte(':')
		v, err := Marshal(kv.V)
		if err != nil {
			return nil, err
		}
		b.Write(v)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// Marshal encodes v compactly without escaping <, > and &, so prompt text in
// request bodies stays readable.
func Marshal(v any) ([]byte, error) {
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	if err := e.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(b.Bytes(), "\n"), nil
}
