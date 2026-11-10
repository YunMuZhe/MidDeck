//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/YunMuZhe/MidDeck/internal/config"
	"github.com/YunMuZhe/MidDeck/internal/diag"
	"github.com/YunMuZhe/MidDeck/internal/driver/kafka"
	"github.com/hamba/avro/v2"
)

func registerSchema(t *testing.T, reg, subject, typ, schema string) int {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"schema": schema, "schemaType": typ})
	resp, err := http.Post(reg+"/subjects/"+subject+"/versions", "application/vnd.schemaregistry.v1+json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	var out struct{ ID int }
	if resp.StatusCode != 200 || json.Unmarshal(b, &out) != nil {
		t.Fatalf("register %s: %s", subject, b)
	}
	t.Cleanup(func() {
		for _, q := range []string{"", "?permanent=true"} {
			req, _ := http.NewRequest(http.MethodDelete, reg+"/subjects/"+subject+q, nil)
			if r, err := http.DefaultClient.Do(req); err == nil {
				r.Body.Close()
			}
		}
	})
	return out.ID
}

// TestKafkaDecode produces Schema Registry framed Avro and Protobuf records and reads
// them back through a session whose registry is configured like a user would.
func TestKafkaDecode(t *testing.T) {
	reg := env(t, "MIDDECK_REGISTRY")
	addr := env(t, "MIDDECK_KAFKA")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	n := time.Now().UnixNano()
	topic := fmt.Sprintf("middeck-decode-%d", n)

	avroSchema := `{"type":"record","name":"Order","namespace":"it","fields":[{"name":"id","type":"long"},{"name":"amount","type":{"type":"bytes","logicalType":"decimal","precision":10,"scale":2}},{"name":"note","type":["null","string"]}]}`
	avroID := registerSchema(t, reg, topic+"-value", "AVRO", avroSchema)
	protoID := registerSchema(t, reg, topic+"-pb-value", "PROTOBUF", `syntax = "proto3"; package it; message A { int32 x = 1; } message B { string name = 1; int64 big = 2; }`)

	rep := &diag.Report{}
	s, err := kafka.Open(ctx, config.Instance{Kind: config.KindKafka, Addrs: []string{addr}, Registry: config.Registry{URL: reg}}, dialer(), rep)
	if err != nil {
		checkReport(t, rep)
		t.Fatal(err)
	}
	defer s.Close()
	found := false
	for _, st := range rep.Steps {
		if st.Name == "Schema Registry" {
			found = st.Status == diag.OK
		}
	}
	if !found {
		t.Fatalf("no OK registry step in %+v", rep.Steps)
	}
	if err := s.CreateTopic(ctx, kafka.CreateTopicReq{Name: topic, Partitions: 1, Replication: 1}); err != nil {
		t.Fatal(err)
	}
	defer s.DeleteTopic(ctx, topic)

	sch := avro.MustParse(avroSchema)
	av, err := avro.Marshal(sch, map[string]any{"id": int64(9007199254740993), "amount": []byte{0x04, 0xd2}, "note": nil})
	if err != nil {
		t.Fatal(err)
	}
	hdr := func(id int) []byte {
		b := []byte{0, 0, 0, 0, 0}
		binary.BigEndian.PutUint32(b[1:], uint32(id))
		return b
	}
	// B{name:"bee", big:1}: second message in the file → index path [1] → zigzag 1 count, 1 → 2.
	pb := append(append(hdr(protoID), 2, 2), 0x0a, 3, 'b', 'e', 'e', 0x10, 1)
	for _, v := range [][]byte{append(hdr(avroID), av...), pb, append(hdr(424242), 1, 2, 3)} {
		if _, err := s.Produce(ctx, kafka.ProduceReq{Topic: topic, Partition: -1, Value: string(v)}); err != nil {
			t.Fatal(err)
		}
	}
	msgs, err := s.Fetch(ctx, kafka.FetchReq{Topic: topic, Partition: -1, Mode: "oldest", Limit: 10})
	if err != nil || len(msgs) != 3 {
		t.Fatalf("%d %v", len(msgs), err)
	}
	byOffset := map[int64]kafka.Message{}
	for _, m := range msgs {
		byOffset[m.Offset] = m
	}
	if m := byOffset[0]; m.ValueFormat != "avro" || m.Value != `{"id":9007199254740993,"amount":"12.34","note":null}` || !strings.Contains(m.ValueSchema, topic+"-value v1") {
		t.Fatalf("avro: %+v", m)
	}
	if m := byOffset[1]; m.ValueFormat != "protobuf" || m.Value != `{"name":"bee","big":"1"}` || !strings.HasSuffix(m.ValueSchema, "it.B") {
		t.Fatalf("protobuf: %+v", m)
	}
	if m := byOffset[2]; m.ValueFormat != "binary" || !strings.Contains(m.DecodeError, "424242") {
		t.Fatalf("unknown id: %+v", m)
	}
	// Filters see decoded text; raw mode skips decoding.
	msgs, _ = s.Fetch(ctx, kafka.FetchReq{Topic: topic, Partition: -1, Mode: "oldest", Limit: 10, Contains: `"name":"bee"`})
	if len(msgs) != 1 {
		t.Fatalf("contains on decoded value: %d", len(msgs))
	}
	msgs, _ = s.Fetch(ctx, kafka.FetchReq{Topic: topic, Partition: 0, Mode: "offset", Offset: 0, Limit: 1, Decode: "raw"})
	if len(msgs) != 1 || msgs[0].ValueFormat != "binary" {
		t.Fatalf("raw: %+v", msgs)
	}
	subs, err := s.Decoder().Registry().Subjects(ctx)
	if err != nil {
		t.Fatal(err)
	}
	ok := false
	for _, x := range subs {
		ok = ok || (x.Name == topic+"-pb-value" && x.Type == "PROTOBUF" && x.Latest == 1)
	}
	if !ok {
		t.Fatalf("subjects %+v", subs)
	}
}
