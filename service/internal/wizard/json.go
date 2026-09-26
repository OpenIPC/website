package wizard

import (
	"bytes"
	"encoding/json"
)

// obj is a JSON object that keeps its keys in the order they were set, so the
// document reads in the order the page uses it and equal content encodes
// equally (which the block pools rely on).
type obj []kv

type kv struct {
	k string
	v any
}

func (o *obj) set(k string, v any) { *o = append(*o, kv{k, v}) }

func (o obj) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, p := range o {
		if i > 0 {
			buf.WriteByte(',')
		}
		encode(&buf, p.k)
		buf.WriteByte(':')
		encode(&buf, p.v)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// encode writes v as compact JSON. HTML escaping is off: the document is
// served as application/json and the commands in it contain < and >.
func encode(buf *bytes.Buffer, v any) {
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		panic("wizard: " + err.Error())
	}
	buf.Write(bytes.TrimSuffix(out.Bytes(), []byte("\n")))
}

// key is a stable identity for pooling equal values.
func key(v any) string {
	var buf bytes.Buffer
	encode(&buf, v)
	return buf.String()
}
