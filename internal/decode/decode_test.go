package decode

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/YunMuZhe/MidDeck/internal/config"
	"github.com/hamba/avro/v2"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"
)

const moneyAvro = `{"type":"record","name":"Money","namespace":"acme","fields":[
 {"name":"amount","type":{"type":"bytes","logicalType":"decimal","precision":10,"scale":2}},
 {"name":"currency","type":"string"}]}`

const orderAvro = `{"type":"record","name":"Order","namespace":"acme","fields":[
 {"name":"id","type":"long"},
 {"name":"note","type":["null","string"],"default":null},
 {"name":"total","type":"acme.Money"},
 {"name":"items","type":{"type":"array","items":"string"}}]}`

const commonProto = `syntax = "proto3";
package acme.common;
message Money { string currency = 1; int64 cents = 2; }`

const orderProto = `syntax = "proto3";
package acme;
import "common.proto";
import "google/protobuf/timestamp.proto";
message Unused { int32 x = 1; }
message Envelope {
  message Order { int64 id = 1; acme.common.Money total = 2; google.protobuf.Timestamp at = 3; }
}`

type fakeRegistry struct {
	*httptest.Server
	hits atomic.Int32
}

func newFakeRegistry(t *testing.T) *fakeRegistry {
	f := &fakeRegistry{}
	ref := func(name, subject string) []map[string]any {
		return []map[string]any{{"name": name, "subject": subject, "version": 1}}
	}
	ids := map[string]any{
		"1": map[string]any{"schema": moneyAvro},
		"2": map[string]any{"schema": orderAvro, "references": ref("acme.Money", "money")},
		"3": map[string]any{"schemaType": "PROTOBUF", "schema": orderProto, "references": ref("common.proto", "common")},
		"4": map[string]any{"schemaType": "JSON", "schema": `{"type":"object"}`},
	}
	versions := map[string]any{
		"money/1":  map[string]any{"subject": "money", "version": 1, "id": 1, "schema": moneyAvro},
		"common/1": map[string]any{"subject": "common", "version": 1, "id": 5, "schemaType": "PROTOBUF", "schema": commonProto},
	}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.hits.Add(1)
		if u, p, _ := r.BasicAuth(); u != "key" || p != "secret" {
			w.WriteHeader(401)
			fmt.Fprint(w, `{"error_code":401,"message":"Unauthorized"}`)
			return
		}
		p := strings.TrimPrefix(r.URL.Path, "/")
		var out any
		switch {
		case p == "subjects":
			out = []string{"orders-value", "money", "common"}
		case strings.HasPrefix(p, "schemas/ids/") && strings.HasSuffix(p, "/versions"):
			id := strings.TrimSuffix(strings.TrimPrefix(p, "schemas/ids/"), "/versions")
			if id == "2" {
				out = []map[string]any{{"subject": "orders-value", "version": 3}}
			} else {
				out = []any{}
			}
		case strings.HasPrefix(p, "schemas/ids/"):
			out = ids[strings.TrimPrefix(p, "schemas/ids/")]
		case strings.HasPrefix(p, "subjects/"):
			parts := strings.Split(strings.TrimPrefix(p, "subjects/"), "/")
			if len(parts) == 3 {
				out = versions[parts[0]+"/"+parts[2]]
			}
		}
		if out == nil {
			w.WriteHeader(404)
			fmt.Fprint(w, `{"error_code":40403,"message":"Schema not found"}`)
			return
		}
		_ = json.NewEncoder(w).Encode(out)
	}))
	t.Cleanup(f.Close)
	return f
}

func dialer(ctx context.Context, network, addr string) (net.Conn, error) {
	return (&net.Dialer{}).DialContext(ctx, network, addr)
}

func frame(id int, payload []byte, idx ...byte) []byte {
	b := []byte{0, 0, 0, 0, 0}
	binary.BigEndian.PutUint32(b[1:], uint32(id))
	b = append(b, idx...)
	return append(b, payload...)
}

func TestRegistryAvroWithReference(t *testing.T) {
	reg := newFakeRegistry(t)
	d, err := New(config.Instance{Registry: config.Registry{URL: reg.URL + "/", Username: "key", Password: "secret"}}, dialer)
	if err != nil {
		t.Fatal(err)
	}
	cache := &avro.SchemaCache{}
	avro.MustParse(moneyAvro) // sanity
	if _, err := avro.ParseWithCache(moneyAvro, "", cache); err != nil {
		t.Fatal(err)
	}
	sch, err := avro.ParseWithCache(orderAvro, "", cache)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := avro.Marshal(sch, map[string]any{
		"id": int64(42), "note": map[string]any{"string": "rush"},
		"total": map[string]any{"amount": []byte{0x30, 0x39}, "currency": "EUR"}, // 12345 → 123.45
		"items": []any{"a", "b"},
	})
	if err != nil {
		t.Fatal(err)
	}
	r := d.Decode(context.Background(), "orders", false, frame(2, payload), Options{})
	want := `{"id":42,"note":"rush","total":{"amount":"123.45","currency":"EUR"},"items":["a","b"]}`
	if r.Text != want || r.Format != FormatAvro || r.Schema != "orders-value v3 · id 2" || r.Error != "" {
		t.Fatalf("%+v", r)
	}
	before := reg.hits.Load()
	d.Decode(context.Background(), "orders", false, frame(2, payload), Options{})
	if reg.hits.Load() != before {
		t.Fatal("schema not cached")
	}
	// Unknown ID: falls back, explains, and is negatively cached.
	r = d.Decode(context.Background(), "orders", false, frame(99, []byte("x")), Options{})
	if r.Format != FormatBinary || !strings.Contains(r.Error, "99 not found") {
		t.Fatalf("%+v", r)
	}
	before = reg.hits.Load()
	d.Decode(context.Background(), "orders", false, frame(99, []byte("x")), Options{})
	if reg.hits.Load() != before {
		t.Fatal("missing id not negatively cached")
	}
}

func compileForTest(t *testing.T, dir string) *localProtos {
	l := newLocalProtos([]string{dir})
	if names, problems := l.types(); len(problems) > 0 || len(names) == 0 {
		t.Fatalf("names %v problems %v", names, problems)
	}
	return l
}

func encodeOrder(t *testing.T, l *localProtos, typ string) []byte {
	_, md, err := l.find(typ)
	if err != nil {
		t.Fatal(err)
	}
	m := dynamicpb.NewMessage(md)
	m.Set(md.Fields().ByName("id"), protoValueInt(7))
	total := dynamicpb.NewMessage(md.Fields().ByName("total").Message())
	total.Set(total.Descriptor().Fields().ByName("currency"), protoValueString("USD"))
	total.Set(total.Descriptor().Fields().ByName("cents"), protoValueInt(1999))
	m.Set(md.Fields().ByName("total"), protoValueMsg(total))
	b, err := proto.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func writeProtos(t *testing.T) string {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "common.proto"), []byte(commonProto), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "acme"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "acme", "order.proto"), []byte(orderProto), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestRegistryProtobufNestedWithReference(t *testing.T) {
	reg := newFakeRegistry(t)
	d, _ := New(config.Instance{Registry: config.Registry{URL: reg.URL, Username: "key", Password: "secret"}}, dialer)
	l := compileForTest(t, writeProtos(t))
	payload := encodeOrder(t, l, "acme.Envelope.Order")
	// Message index path [1, 0]: second top-level message, its first nested message.
	r := d.Decode(context.Background(), "orders", false, frame(3, payload, 4, 2, 0), Options{})
	if r.Format != FormatProtobuf || r.Text != `{"id":"7","total":{"currency":"USD","cents":"1999"}}` || !strings.HasSuffix(r.Schema, "acme.Envelope.Order") {
		t.Fatalf("%+v", r)
	}
	// JSON Schema: payload is the JSON itself.
	r = d.Decode(context.Background(), "t", true, frame(4, []byte(`{"a":1}`)), Options{})
	if r.Format != FormatJSON || r.Text != `{"a":1}` || r.Schema != "id 4" {
		t.Fatalf("%+v", r)
	}
}

func TestLocalProtoBindings(t *testing.T) {
	dir := writeProtos(t)
	d, _ := New(config.Instance{Proto: config.Proto{Paths: []string{dir}, Bindings: []config.ProtoBinding{{Topic: "orders.*", Value: "acme.Envelope.Order"}}}}, dialer)
	names, _ := d.ProtoTypes(false)
	if strings.Join(names, ",") != "acme.Envelope,acme.Envelope.Order,acme.Unused,acme.common.Money" {
		t.Fatalf("types %v", names)
	}
	l := compileForTest(t, dir)
	payload := encodeOrder(t, l, "acme.Envelope.Order")
	want := `{"id":"7","total":{"currency":"USD","cents":"1999"}}`
	if r := d.Decode(context.Background(), "orders.eu", false, payload, Options{}); r.Text != want || r.Schema != "acme.Envelope.Order" {
		t.Fatalf("%+v", r)
	}
	// Confluent-framed bytes but no registry here: the binding still decodes.
	if r := d.Decode(context.Background(), "orders.eu", false, frame(3, payload, 0), Options{}); r.Text != want {
		t.Fatalf("%+v", r)
	}
	// Unbound topic: binary fallback.
	if r := d.Decode(context.Background(), "payments", false, payload, Options{}); r.Format == FormatProtobuf {
		t.Fatalf("%+v", r)
	}
	// Explicit type override, and a wrong type is reported rather than shown as garbage.
	if r := d.Decode(context.Background(), "payments", false, payload, Options{ProtoType: "acme.Envelope.Order"}); r.Text != want {
		t.Fatalf("%+v", r)
	}
	if r := d.Decode(context.Background(), "orders.eu", false, []byte("plain text"), Options{}); r.Format != FormatText || r.Error == "" {
		t.Fatalf("%+v", r)
	}
	if r := d.Decode(context.Background(), "orders.eu", false, payload, Options{Raw: true}); r.Format == FormatProtobuf {
		t.Fatalf("%+v", r)
	}
}

func TestPlainAndHints(t *testing.T) {
	d, _ := New(config.Instance{}, dialer)
	ctx := context.Background()
	cases := []struct {
		in, format, text string
	}{
		{`{"a":1}`, FormatJSON, `{"a":1}`},
		{"hello", FormatText, "hello"},
		{"\xff\x00", FormatBinary, "/wA="},
		{"", "", ""},
	}
	for _, c := range cases {
		if r := d.Decode(ctx, "t", false, []byte(c.in), Options{}); r.Format != c.format || r.Text != c.text {
			t.Fatalf("%q → %+v", c.in, r)
		}
	}
	r := d.Decode(ctx, "t", false, frame(12, []byte{2, 4}), Options{})
	if !strings.Contains(r.Error, "schema id 12") || r.Format != FormatBinary {
		t.Fatalf("%+v", r)
	}
	if _, err := New(config.Instance{Registry: config.Registry{URL: "localhost:8081"}}, dialer); err == nil {
		t.Fatal("URL without scheme accepted")
	}
}

func TestRegistryAuthError(t *testing.T) {
	reg := newFakeRegistry(t)
	d, _ := New(config.Instance{Registry: config.Registry{URL: reg.URL}}, dialer)
	if _, err := d.Registry().Ping(context.Background()); err == nil || !strings.Contains(err.Error(), "Unauthorized") {
		t.Fatal(err)
	}
	// Credentials in the URL work too and are not kept in it.
	d, _ = New(config.Instance{Registry: config.Registry{URL: strings.Replace(reg.URL, "http://", "http://key:secret@", 1)}}, dialer)
	if n, err := d.Registry().Ping(context.Background()); err != nil || n != 3 || strings.Contains(d.Registry().URL(), "secret") {
		t.Fatal(n, err, d.Registry().URL())
	}
	subs, err := d.Registry().Subjects(context.Background())
	if err != nil || len(subs) != 3 {
		t.Fatal(subs, err)
	}
}

func protoValueInt(v int64) protoreflect.Value              { return protoreflect.ValueOfInt64(v) }
func protoValueString(v string) protoreflect.Value          { return protoreflect.ValueOfString(v) }
func protoValueMsg(m *dynamicpb.Message) protoreflect.Value { return protoreflect.ValueOfMessage(m) }

func TestRegistryDownIsRememberedBriefly(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close() // nothing listens: connection refused
	var dials atomic.Int32
	d, _ := New(config.Instance{Registry: config.Registry{URL: "http://" + addr}}, func(ctx context.Context, n, a string) (net.Conn, error) {
		dials.Add(1)
		return dialer(ctx, n, a)
	})
	for id := 1; id <= 5; id++ {
		r := d.Decode(context.Background(), "t", false, frame(id, []byte("x")), Options{})
		if r.Format != FormatBinary || !strings.Contains(r.Error, "unreachable") && id > 1 {
			t.Fatalf("%d: %+v", id, r)
		}
	}
	if dials.Load() != 1 {
		t.Fatalf("dialed %d times", dials.Load())
	}
}
