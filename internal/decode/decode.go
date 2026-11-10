package decode

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/YunMuZhe/MidDeck/internal/config"
	"github.com/hamba/avro/v2"
)

// Formats reported in Result.Format.
const (
	FormatText     = "text"
	FormatJSON     = "json"
	FormatAvro     = "avro"
	FormatProtobuf = "protobuf"
	FormatBinary   = "binary" // Text is base64
)

// Result is one decoded key or value.
type Result struct {
	Text   string `json:"text"`
	Format string `json:"format,omitempty"` // "" for an empty/absent payload
	// Schema describes what was used, e.g. "orders-value v3 · id 12" or "acme.Order".
	Schema string `json:"schema,omitempty"`
	// Error explains why a payload that looked encoded could not be decoded.
	Error string `json:"error,omitempty"`
}

// Decoder decodes records of one Kafka instance. It is safe for concurrent use.
type Decoder struct {
	reg   *Registry
	proto *localProtos

	mu       sync.Mutex
	bindings []config.ProtoBinding
	avro     map[int]avro.Schema
	pb       map[int]*protoFiles
}

// New builds a decoder. A malformed registry URL is returned as an error, but the
// decoder is still usable for the other formats.
func New(inst config.Instance, dial DialFunc) (*Decoder, error) {
	d := &Decoder{
		proto:    newLocalProtos(inst.Proto.Paths),
		bindings: inst.Proto.Bindings,
		avro:     map[int]avro.Schema{},
		pb:       map[int]*protoFiles{},
	}
	if strings.TrimSpace(inst.Registry.URL) != "" {
		r, err := NewRegistry(inst.Registry, dial)
		if err != nil {
			return d, err
		}
		d.reg = r
	}
	return d, nil
}

// Registry returns the schema registry client, or nil when none is configured.
func (d *Decoder) Registry() *Registry { return d.reg }

// Close releases resources.
func (d *Decoder) Close() {
	if d.reg != nil {
		d.reg.Close()
	}
}

// SetBindings replaces the topic → Protobuf type bindings.
func (d *Decoder) SetBindings(b []config.ProtoBinding) {
	d.mu.Lock()
	d.bindings = b
	d.mu.Unlock()
}

// ProtoTypes lists message types found in the local .proto files, plus compile problems.
func (d *Decoder) ProtoTypes(reload bool) ([]string, []string) {
	if reload {
		d.proto.reset()
	}
	return d.proto.types()
}

// binding returns the Protobuf type bound to topic for the key or the value.
func (d *Decoder) binding(topic string, isKey bool) string {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, b := range d.bindings {
		if b.Topic == topic || (strings.ContainsAny(b.Topic, "*?[") && match(b.Topic, topic)) {
			if isKey {
				return b.Key
			}
			return b.Value
		}
	}
	return ""
}

func match(pattern, s string) bool {
	ok, _ := path.Match(pattern, s)
	return ok
}

// Options tweak one Decode call.
type Options struct {
	// Raw skips schema decoding: UTF-8 text as is, everything else base64.
	Raw bool
	// ProtoType forces a local Protobuf type, overriding bindings.
	ProtoType string
}

// Decode turns b into display text; a nil Decoder only does text / base64. It never fails: problems end up in Result.Error
// and the payload falls back to text or base64.
func (d *Decoder) Decode(ctx context.Context, topic string, isKey bool, b []byte, o Options) Result {
	if len(b) == 0 {
		return Result{}
	}
	if d == nil || o.Raw {
		return plain(b)
	}
	var regErr string
	if id, payload, ok := confluentFrame(b); ok {
		if d.reg != nil {
			r, err := d.viaRegistry(ctx, id, payload)
			if err == nil {
				return r
			}
			regErr = err.Error()
		} else {
			regErr = fmt.Sprintf("looks like Schema Registry framing (schema id %d), but no registry is configured for this connection", id)
		}
	}
	typ := o.ProtoType
	if typ == "" {
		typ = d.binding(topic, isKey)
	}
	if typ != "" {
		r, err := d.viaLocalProto(typ, b)
		if err == nil {
			return r
		}
		if regErr == "" {
			regErr = err.Error()
		}
	}
	r := plain(b)
	r.Error = regErr
	return r
}

func plain(b []byte) Result {
	if isText(b) {
		f := FormatText
		if json.Valid(b) {
			f = FormatJSON
		}
		return Result{Text: string(b), Format: f}
	}
	return Result{Text: base64.StdEncoding.EncodeToString(b), Format: FormatBinary}
}

// isText reports valid UTF-8 without control characters other than whitespace.
func isText(b []byte) bool {
	if !utf8.Valid(b) {
		return false
	}
	for _, c := range b {
		if c < 0x20 && c != '\t' && c != '\n' && c != '\r' || c == 0x7f {
			return false
		}
	}
	return true
}

// confluentFrame splits the Confluent wire format: magic 0x00 + big-endian schema ID.
func confluentFrame(b []byte) (int, []byte, bool) {
	if len(b) < 5 || b[0] != 0 {
		return 0, nil, false
	}
	id := int(binary.BigEndian.Uint32(b[1:5]))
	if id <= 0 {
		return 0, nil, false
	}
	return id, b[5:], true
}

func label(s *Schema) string {
	if s.Subject != "" {
		return fmt.Sprintf("%s v%d · id %d", s.Subject, s.Version, s.ID)
	}
	return fmt.Sprintf("id %d", s.ID)
}

func (d *Decoder) viaRegistry(ctx context.Context, id int, payload []byte) (Result, error) {
	s, err := d.reg.ByID(ctx, id)
	if err != nil {
		return Result{}, err
	}
	switch s.Type {
	case "AVRO":
		sch, err := d.avroSchema(ctx, s)
		if err != nil {
			return Result{}, fmt.Errorf("avro schema %s: %w", label(s), err)
		}
		var v any
		if err := avro.Unmarshal(sch, payload, &v); err != nil {
			return Result{}, fmt.Errorf("avro decode with %s: %w", label(s), err)
		}
		out, err := avroJSON(sch, v)
		if err != nil {
			return Result{}, err
		}
		return Result{Text: string(out), Format: FormatAvro, Schema: label(s)}, nil
	case "PROTOBUF":
		files, err := d.protoForSchema(ctx, s)
		if err != nil {
			return Result{}, fmt.Errorf("protobuf schema %s: %w", label(s), err)
		}
		idx, rest, err := messageIndexes(payload)
		if err != nil {
			return Result{}, err
		}
		md, err := files.byIndexes(idx)
		if err != nil {
			return Result{}, fmt.Errorf("%s: %w", label(s), err)
		}
		txt, err := files.toJSON(md, rest)
		if err != nil {
			return Result{}, fmt.Errorf("protobuf decode %s with %s: %w", md.FullName(), label(s), err)
		}
		return Result{Text: txt, Format: FormatProtobuf, Schema: label(s) + " · " + string(md.FullName())}, nil
	case "JSON":
		r := plain(payload)
		if r.Format != FormatJSON {
			return Result{}, fmt.Errorf("payload for JSON schema %s is not JSON", label(s))
		}
		r.Schema = label(s)
		return r, nil
	}
	return Result{}, fmt.Errorf("unsupported schema type %s (%s)", s.Type, label(s))
}

func (d *Decoder) viaLocalProto(typ string, b []byte) (Result, error) {
	files, md, err := d.proto.find(typ)
	if err != nil {
		return Result{}, err
	}
	txt, err := files.toJSON(md, b)
	if err != nil {
		// Producers that use the Confluent serializer without a registry configured here.
		if _, payload, ok := confluentFrame(b); ok {
			if _, rest, e := messageIndexes(payload); e == nil {
				if t, e := files.toJSON(md, rest); e == nil {
					return Result{Text: t, Format: FormatProtobuf, Schema: typ}, nil
				}
			}
		}
		return Result{}, fmt.Errorf("protobuf decode as %s: %w", typ, err)
	}
	return Result{Text: txt, Format: FormatProtobuf, Schema: typ}, nil
}

// messageIndexes reads the Confluent Protobuf message-index path: a zigzag varint
// count followed by that many zigzag varints; a lone 0 means [0].
func messageIndexes(b []byte) ([]int, []byte, error) {
	n, k := binary.Varint(b)
	if k <= 0 || n < 0 || n > 64 {
		return nil, nil, fmt.Errorf("invalid protobuf message index header")
	}
	b = b[k:]
	if n == 0 {
		return []int{0}, b, nil
	}
	idx := make([]int, n)
	for i := range idx {
		v, k := binary.Varint(b)
		if k <= 0 || v < 0 {
			return nil, nil, fmt.Errorf("invalid protobuf message index header")
		}
		idx[i] = int(v)
		b = b[k:]
	}
	return idx, b, nil
}
