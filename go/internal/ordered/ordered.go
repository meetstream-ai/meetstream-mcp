// Package ordered is a JSON object that keeps insertion order, so request
// payloads and tool output read in the same key order as the Node server.
package ordered

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Object is an insertion-ordered JSON object.
type Object struct {
	keys []string
	vals map[string]any
}

// New returns an empty Object.
func New() *Object { return &Object{vals: map[string]any{}} }

// Set adds k, or replaces its value in place if it already exists.
func (o *Object) Set(k string, v any) *Object {
	if _, ok := o.vals[k]; !ok {
		o.keys = append(o.keys, k)
	}
	o.vals[k] = v
	return o
}

// Get returns the value stored under k.
func (o *Object) Get(k string) (any, bool) {
	v, ok := o.vals[k]
	return v, ok
}

// Len is the number of keys.
func (o *Object) Len() int { return len(o.keys) }

// MarshalJSON writes the keys in insertion order.
func (o *Object) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, k := range o.keys {
		if i > 0 {
			b.WriteByte(',')
		}
		kb, _ := json.Marshal(k)
		b.Write(kb)
		b.WriteByte(':')
		vb, err := json.Marshal(o.vals[k])
		if err != nil {
			return nil, fmt.Errorf("ordered: key %q: %w", k, err)
		}
		b.Write(vb)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// FromRaw decodes the top level of a JSON object, keeping key order. Nested
// values stay as raw JSON. ok is false when raw is not a JSON object.
func FromRaw(raw []byte) (o *Object, ok bool) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	tok, err := dec.Token()
	if err != nil || tok != json.Delim('{') {
		return nil, false
	}
	o = New()
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return nil, false
		}
		k, _ := kt.(string)
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, false
		}
		o.Set(k, v)
	}
	return o, true
}
