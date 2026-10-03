// Package jsjson re-prints JSON exactly as JavaScript's
// JSON.stringify(JSON.parse(text), null, 2) would, so tool output from the Go
// server is byte-identical to the Node server's: numbers in ES number format
// (624.0 → 624, 1e21 → 1e+21), strings escaped the JavaScript way (no \u003c
// for <, no \u2028 escapes), object key order preserved.
package jsjson

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Indent reformats raw JSON. It returns an error if raw is not a single JSON value.
func Indent(raw []byte) (string, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var b strings.Builder
	if err := value(dec, &b, 0); err != nil {
		return "", err
	}
	if _, err := dec.Token(); err != io.EOF {
		return "", errors.New("jsjson: trailing data")
	}
	return b.String(), nil
}

func value(dec *json.Decoder, b *strings.Builder, depth int) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	switch t := tok.(type) {
	case json.Delim:
		if t == '{' {
			return object(dec, b, depth)
		}
		if t == '[' {
			return array(dec, b, depth)
		}
		return errors.New("jsjson: unexpected delimiter")
	case string:
		quote(b, t)
	case json.Number:
		b.WriteString(number(t))
	case bool:
		b.WriteString(strconv.FormatBool(t))
	case nil:
		b.WriteString("null")
	}
	return nil
}

func object(dec *json.Decoder, b *strings.Builder, depth int) error {
	// JSON.parse keeps the last value for a repeated key, at the first key's position.
	type kv struct {
		k string
		v string
	}
	var items []kv
	index := map[string]int{}
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return err
		}
		k, _ := kt.(string)
		var vb strings.Builder
		if err := value(dec, &vb, depth+1); err != nil {
			return err
		}
		if i, ok := index[k]; ok {
			items[i].v = vb.String()
			continue
		}
		index[k] = len(items)
		items = append(items, kv{k, vb.String()})
	}
	if _, err := dec.Token(); err != nil { // '}'
		return err
	}
	if len(items) == 0 {
		b.WriteString("{}")
		return nil
	}
	b.WriteString("{\n")
	for i, it := range items {
		pad(b, depth+1)
		quote(b, it.k)
		b.WriteString(": ")
		b.WriteString(it.v)
		if i < len(items)-1 {
			b.WriteByte(',')
		}
		b.WriteByte('\n')
	}
	pad(b, depth)
	b.WriteByte('}')
	return nil
}

func array(dec *json.Decoder, b *strings.Builder, depth int) error {
	var items []string
	for dec.More() {
		var vb strings.Builder
		if err := value(dec, &vb, depth+1); err != nil {
			return err
		}
		items = append(items, vb.String())
	}
	if _, err := dec.Token(); err != nil { // ']'
		return err
	}
	if len(items) == 0 {
		b.WriteString("[]")
		return nil
	}
	b.WriteString("[\n")
	for i, it := range items {
		pad(b, depth+1)
		b.WriteString(it)
		if i < len(items)-1 {
			b.WriteByte(',')
		}
		b.WriteByte('\n')
	}
	pad(b, depth)
	b.WriteByte(']')
	return nil
}

func pad(b *strings.Builder, depth int) {
	for i := 0; i < depth; i++ {
		b.WriteString("  ")
	}
}

// number formats like JavaScript's Number#toString. encoding/json already
// uses the ES6 algorithm for float64, so round-trip through it.
func number(n json.Number) string {
	f, err := strconv.ParseFloat(string(n), 64)
	if err != nil {
		return string(n)
	}
	if f == 0 { // JSON.stringify(-0) is "0"
		return "0"
	}
	out, err := json.Marshal(f)
	if err != nil { // ±Inf from an overflowing literal: JSON.stringify writes null
		return "null"
	}
	return string(out)
}

// quote escapes like JSON.stringify: ", \, \b \f \n \r \t, other control
// characters as \u00XX; everything else (including <, >, &, U+2028) literal.
func quote(b *strings.Builder, s string) {
	const hex = "0123456789abcdef"
	b.WriteByte('"')
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == '"':
			b.WriteString(`\"`)
		case r == '\\':
			b.WriteString(`\\`)
		case r == '\b':
			b.WriteString(`\b`)
		case r == '\f':
			b.WriteString(`\f`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case r < 0x20:
			b.WriteString(`\u00`)
			b.WriteByte(hex[r>>4])
			b.WriteByte(hex[r&0xf])
		default:
			b.WriteString(s[i : i+size])
		}
		i += size
	}
	b.WriteByte('"')
}
