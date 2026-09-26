package wizard

import (
	"bytes"
	"fmt"
	"strconv"
	"unicode/utf8"
)

// The export is compared byte for byte with what Ruby's JSON.generate wrote,
// so it is written by hand rather than through encoding/json: keys in the
// order they were added (Ruby hashes are ordered, Go maps are not) and strings
// escaped the way the json gem escapes them -- quote, backslash and control
// characters only; no HTML escaping, and no escaping of non-ASCII.

// obj is an ordered JSON object.
type obj []kv

type kv struct {
	k string
	v any
}

func (o *obj) set(k string, v any) { *o = append(*o, kv{k, v}) }

func encode(buf *bytes.Buffer, v any) {
	switch t := v.(type) {
	case nil:
		buf.WriteString("null")
	case bool:
		buf.WriteString(strconv.FormatBool(t))
	case int:
		buf.WriteString(strconv.Itoa(t))
	case string:
		encodeString(buf, t)
	case []string:
		buf.WriteByte('[')
		for i, s := range t {
			if i > 0 {
				buf.WriteByte(',')
			}
			encodeString(buf, s)
		}
		buf.WriteByte(']')
	case []any:
		buf.WriteByte('[')
		for i, e := range t {
			if i > 0 {
				buf.WriteByte(',')
			}
			encode(buf, e)
		}
		buf.WriteByte(']')
	case obj:
		buf.WriteByte('{')
		for i, p := range t {
			if i > 0 {
				buf.WriteByte(',')
			}
			encodeString(buf, p.k)
			buf.WriteByte(':')
			encode(buf, p.v)
		}
		buf.WriteByte('}')
	case *obj:
		encode(buf, *t)
	default:
		panic(fmt.Sprintf("wizard: cannot encode %T", v))
	}
}

func encodeString(buf *bytes.Buffer, s string) {
	buf.WriteByte('"')
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == '"':
			buf.WriteString(`\"`)
		case r == '\\':
			buf.WriteString(`\\`)
		case r == '\b':
			buf.WriteString(`\b`)
		case r == '\f':
			buf.WriteString(`\f`)
		case r == '\n':
			buf.WriteString(`\n`)
		case r == '\r':
			buf.WriteString(`\r`)
		case r == '\t':
			buf.WriteString(`\t`)
		case r < 0x20:
			fmt.Fprintf(buf, `\u%04x`, r)
		default:
			buf.WriteString(s[i : i+size])
		}
		i += size
	}
	buf.WriteByte('"')
}

// key is a stable identity for pooling equal values.
func key(v any) string {
	var buf bytes.Buffer
	encode(&buf, v)
	return buf.String()
}
