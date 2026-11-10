package decode

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"reflect"
	"sort"

	"github.com/hamba/avro/v2"
)

// avroSchema parses a registry schema, resolving its references first. Every schema
// ID gets its own named-type cache: two versions may define the same name differently.
func (d *Decoder) avroSchema(ctx context.Context, s *Schema) (avro.Schema, error) {
	d.mu.Lock()
	sch, ok := d.avro[s.ID]
	d.mu.Unlock()
	if ok {
		return sch, nil
	}
	cache := &avro.SchemaCache{}
	seen := map[string]bool{}
	var parseRefs func(refs []Reference, depth int) error
	parseRefs = func(refs []Reference, depth int) error {
		if depth > 16 {
			return fmt.Errorf("schema references nested too deeply")
		}
		for _, ref := range refs {
			key := fmt.Sprintf("%s/%d", ref.Subject, ref.Version)
			if seen[key] {
				continue
			}
			seen[key] = true
			rs, err := d.reg.Version(ctx, ref.Subject, fmt.Sprint(ref.Version))
			if err != nil {
				return fmt.Errorf("reference %s: %w", ref.Name, err)
			}
			if err := parseRefs(rs.References, depth+1); err != nil {
				return err
			}
			if _, err := avro.ParseWithCache(rs.Schema, "", cache); err != nil {
				return fmt.Errorf("reference %s: %w", ref.Name, err)
			}
		}
		return nil
	}
	if err := parseRefs(s.References, 0); err != nil {
		return nil, err
	}
	sch, err := avro.ParseWithCache(s.Schema, "", cache)
	if err != nil {
		return nil, err
	}
	d.mu.Lock()
	d.avro[s.ID] = sch
	d.mu.Unlock()
	return sch, nil
}

// avroJSON renders a generically decoded Avro value as JSON, keeping record field
// order and printing decimals as exact strings.
func avroJSON(s avro.Schema, v any) ([]byte, error) {
	var buf bytes.Buffer
	if err := writeAvro(&buf, s, v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func writeAvro(buf *bytes.Buffer, s avro.Schema, v any) error {
	if v == nil {
		buf.WriteString("null")
		return nil
	}
	switch t := s.(type) {
	case *avro.RefSchema:
		return writeAvro(buf, t.Schema(), v)
	case *avro.RecordSchema:
		m, ok := v.(map[string]any)
		if !ok {
			break
		}
		buf.WriteByte('{')
		for i, f := range t.Fields() {
			if i > 0 {
				buf.WriteByte(',')
			}
			k, _ := json.Marshal(f.Name())
			buf.Write(k)
			buf.WriteByte(':')
			if err := writeAvro(buf, f.Type(), m[f.Name()]); err != nil {
				return err
			}
		}
		buf.WriteByte('}')
		return nil
	case *avro.ArraySchema:
		a, ok := v.([]any)
		if !ok {
			break
		}
		buf.WriteByte('[')
		for i, x := range a {
			if i > 0 {
				buf.WriteByte(',')
			}
			if err := writeAvro(buf, t.Items(), x); err != nil {
				return err
			}
		}
		buf.WriteByte(']')
		return nil
	case *avro.MapSchema:
		m, ok := v.(map[string]any)
		if !ok {
			break
		}
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		buf.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				buf.WriteByte(',')
			}
			kb, _ := json.Marshal(k)
			buf.Write(kb)
			buf.WriteByte(':')
			if err := writeAvro(buf, t.Values(), m[k]); err != nil {
				return err
			}
		}
		buf.WriteByte('}')
		return nil
	case *avro.UnionSchema:
		var branches []avro.Schema
		for _, b := range t.Types() {
			if b.Type() != avro.Null {
				branches = append(branches, b)
			}
		}
		if len(branches) == 1 {
			return writeAvro(buf, branches[0], v)
		}
	case *avro.PrimitiveSchema:
		if r, ok := v.(*big.Rat); ok {
			if dl, ok := t.Logical().(*avro.DecimalLogicalSchema); ok {
				return writeJSON(buf, r.FloatString(dl.Scale()))
			}
		}
	case *avro.FixedSchema:
		if r, ok := v.(*big.Rat); ok {
			if dl, ok := t.Logical().(*avro.DecimalLogicalSchema); ok {
				return writeJSON(buf, r.FloatString(dl.Scale()))
			}
		}
	}
	return writeJSON(buf, generic(v))
}

func writeJSON(buf *bytes.Buffer, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	buf.Write(b)
	return nil
}

// generic converts values encoding/json would render badly.
func generic(v any) any {
	switch t := v.(type) {
	case *big.Rat:
		return ratString(t)
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, x := range t {
			out[k] = generic(x)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, x := range t {
			out[i] = generic(x)
		}
		return out
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Array && rv.Type().Elem().Kind() == reflect.Uint8 { // Avro fixed
		b := make([]byte, rv.Len())
		reflect.Copy(reflect.ValueOf(b), rv)
		return base64.StdEncoding.EncodeToString(b)
	}
	return v
}

// ratString prints r exactly with as few decimals as it needs.
func ratString(r *big.Rat) string {
	for scale := 0; scale <= 38; scale++ {
		p := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(scale)), nil)
		if new(big.Int).Mod(p, r.Denom()).Sign() == 0 {
			return r.FloatString(scale)
		}
	}
	return r.RatString()
}
