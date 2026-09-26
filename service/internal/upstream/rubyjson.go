package upstream

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"unicode/utf8"
)

// rubyPretty is Ruby's JSON.pretty_generate as json 2.7.2 writes it -- the
// version on the host that wrote every index so far, and the one the byte
// comparison in testdata/ was made against. It differs from Go's
// encoding/json in ways that change bytes:
//
//   - an empty object is "{\n<indent>}" and an empty array "[\n\n<indent>]";
//   - nothing beyond `"`, `\` and C0 controls is escaped: not < > &, not
//     U+2028/U+2029, not "/";
//   - C0 controls other than \b \f \n \r \t are \u00xx in lower case;
//   - there is no trailing newline.
func rubyPretty(v any) ([]byte, error) {
	var b bytes.Buffer
	if err := writeRuby(&b, v, 0); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

const rubyIndent = "  "

func writeRuby(b *bytes.Buffer, v any, depth int) error {
	switch x := v.(type) {
	case nil:
		b.WriteString("null")
	case *string:
		if x == nil {
			b.WriteString("null")
			return nil
		}
		return writeRubyString(b, *x)
	case string:
		return writeRubyString(b, x)
	case bool:
		b.WriteString(strconv.FormatBool(x))
	case json.RawMessage:
		// A value passed through from upstream (a size). Re-read so that
		// whatever shape it has is written the Ruby way, not copied verbatim.
		// Objects keep their key order, as a Ruby Hash parsed from them does.
		dec := json.NewDecoder(bytes.NewReader(x))
		dec.UseNumber()
		inner, err := decodeOrdered(dec)
		if err != nil {
			return err
		}
		return writeRuby(b, inner, depth)
	case json.Number:
		b.WriteString(x.String())
	case float64:
		// Only reachable for a value carried from a previous file; integers
		// are written as integers, as Ruby parsed them.
		if x == float64(int64(x)) {
			b.WriteString(strconv.FormatInt(int64(x), 10))
		} else {
			b.WriteString(strconv.FormatFloat(x, 'g', -1, 64))
		}
	case int:
		b.WriteString(strconv.Itoa(x))
	case int64:
		b.WriteString(strconv.FormatInt(x, 10))
	case orderedObject:
		return writeRubyObject(b, x.keys, func(k string) any { return x.values[k] }, depth)
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		return writeRubyObject(b, keys, func(k string) any { return x[k] }, depth)
	case []any:
		if len(x) == 0 {
			b.WriteString("[\n\n")
			writeIndent(b, depth)
			b.WriteByte(']')
			return nil
		}
		b.WriteString("[\n")
		for i, item := range x {
			if i > 0 {
				b.WriteString(",\n")
			}
			writeIndent(b, depth+1)
			if err := writeRuby(b, item, depth+1); err != nil {
				return err
			}
		}
		b.WriteByte('\n')
		writeIndent(b, depth)
		b.WriteByte(']')
	default:
		return fmt.Errorf("rubyPretty: cannot write %T", v)
	}
	return nil
}

func writeRubyObject(b *bytes.Buffer, keys []string, value func(string) any, depth int) error {
	if len(keys) == 0 {
		b.WriteString("{\n")
		writeIndent(b, depth)
		b.WriteByte('}')
		return nil
	}
	b.WriteString("{\n")
	for i, k := range keys {
		if i > 0 {
			b.WriteString(",\n")
		}
		writeIndent(b, depth+1)
		if err := writeRubyString(b, k); err != nil {
			return err
		}
		b.WriteString(": ")
		if err := writeRuby(b, value(k), depth+1); err != nil {
			return err
		}
	}
	b.WriteByte('\n')
	writeIndent(b, depth)
	b.WriteByte('}')
	return nil
}

func writeIndent(b *bytes.Buffer, depth int) {
	for range depth {
		b.WriteString(rubyIndent)
	}
}

// decodeOrdered reads one value, keeping object keys in document order.
func decodeOrdered(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	d, ok := tok.(json.Delim)
	if !ok {
		return tok, nil
	}
	switch d {
	case '{':
		o := orderedObject{}
		for dec.More() {
			k, err := dec.Token()
			if err != nil {
				return nil, err
			}
			v, err := decodeOrdered(dec)
			if err != nil {
				return nil, err
			}
			o.set(k.(string), v)
		}
		_, err := dec.Token()
		return o, err
	case '[':
		a := []any{}
		for dec.More() {
			v, err := decodeOrdered(dec)
			if err != nil {
				return nil, err
			}
			a = append(a, v)
		}
		_, err := dec.Token()
		return a, err
	}
	return nil, fmt.Errorf("rubyPretty: unexpected %v", d)
}

func writeRubyString(b *bytes.Buffer, s string) error {
	if !utf8.ValidString(s) {
		// Ruby's generator raises on invalid UTF-8 rather than guess.
		return fmt.Errorf("rubyPretty: %q is not valid UTF-8", s)
	}
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if c < 0x20 {
				fmt.Fprintf(b, `\u%04x`, c)
			} else {
				b.WriteByte(c)
			}
		}
	}
	b.WriteByte('"')
	return nil
}
