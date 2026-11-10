// Command seeddecode fills the dev playground with schema-encoded messages so the
// message decoding features have something to show:
//
//   - order-avro     Avro, Schema Registry (record with a referenced Money type)
//
//   - payment-proto  Protobuf, Schema Registry (with an imported common.proto)
//
//   - shipment-pb    plain Protobuf without registry; bind it to
//     acme.logistics.Shipment using the .proto files in dev/protos
//
//     go run ./dev/seeddecode            (from the repository root, playground running)
package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/big"
	"math/rand/v2"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/bufbuild/protocompile"
	"github.com/hamba/avro/v2"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"
)

const (
	registry  = "http://localhost:8081"
	bootstrap = "localhost:9092"
	protoRoot = "dev/protos"
)

const moneyAvro = `{"type":"record","name":"Money","namespace":"acme","fields":[
  {"name":"amount","type":{"type":"bytes","logicalType":"decimal","precision":12,"scale":2}},
  {"name":"currency","type":"string"}]}`

const orderAvro = `{"type":"record","name":"Order","namespace":"acme.orders","fields":[
  {"name":"orderId","type":"string"},
  {"name":"userId","type":"long"},
  {"name":"total","type":"acme.Money"},
  {"name":"status","type":{"type":"enum","name":"Status","symbols":["CREATED","PAID","SHIPPED","CANCELLED"]}},
  {"name":"items","type":{"type":"array","items":{"type":"record","name":"Item","fields":[
    {"name":"sku","type":"string"},{"name":"qty","type":"int"}]}}},
  {"name":"note","type":["null","string"],"default":null},
  {"name":"createdAt","type":{"type":"long","logicalType":"timestamp-millis"}}]}`

type ref struct {
	Name    string `json:"name"`
	Subject string `json:"subject"`
	Version int    `json:"version"`
}

func register(subject, typ, schema string, refs []ref) int {
	body, _ := json.Marshal(map[string]any{"schema": schema, "schemaType": typ, "references": refs})
	resp, err := http.Post(registry+"/subjects/"+url.PathEscape(subject)+"/versions", "application/vnd.schemaregistry.v1+json", bytes.NewReader(body))
	if err != nil {
		log.Fatalf("schema registry at %s: %v (is the playground running?)", registry, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	var out struct{ ID int }
	if resp.StatusCode != 200 || json.Unmarshal(b, &out) != nil {
		log.Fatalf("register %s: %s", subject, b)
	}
	fmt.Printf("registered %-28s id %d\n", subject, out.ID)
	return out.ID
}

func frame(id int, payload []byte, indexes ...byte) []byte {
	b := make([]byte, 5, 5+len(indexes)+len(payload))
	binary.BigEndian.PutUint32(b[1:], uint32(id))
	return append(append(b, indexes...), payload...)
}

func main() {
	ctx := context.Background()
	cl, err := kgo.NewClient(kgo.SeedBrokers(bootstrap), kgo.DefaultProduceTopic("order-avro"))
	if err != nil {
		log.Fatal(err)
	}
	defer cl.Close()
	adm := kadm.NewClient(cl)
	for t, p := range map[string]int32{"order-avro": 3, "payment-proto": 3, "shipment-pb": 2} {
		if _, err := adm.CreateTopic(ctx, p, 1, nil, t); err != nil {
			fmt.Printf("topic %s: %v\n", t, err)
		}
	}

	// Avro with a reference.
	register("acme.Money", "AVRO", moneyAvro, nil)
	orderID := register("order-avro-value", "AVRO", orderAvro, []ref{{Name: "acme.Money", Subject: "acme.Money", Version: 1}})
	cache := &avro.SchemaCache{}
	if _, err := avro.ParseWithCache(moneyAvro, "", cache); err != nil {
		log.Fatal(err)
	}
	orderSchema, err := avro.ParseWithCache(orderAvro, "", cache)
	if err != nil {
		log.Fatal(err)
	}
	statuses := []string{"CREATED", "PAID", "SHIPPED", "CANCELLED"}
	var recs []*kgo.Record
	for i := 1; i <= 60; i++ {
		var note any
		if i%7 == 0 {
			note = map[string]any{"string": "gift wrap"}
		}
		v, err := avro.Marshal(orderSchema, map[string]any{
			"orderId": fmt.Sprintf("o-%d", 5000+i), "userId": int64(1000 + rand.IntN(9000)),
			"total":  map[string]any{"amount": big.NewRat(int64(100+rand.IntN(99900)), 100), "currency": "CNY"},
			"status": statuses[i%len(statuses)],
			"items":  []any{map[string]any{"sku": fmt.Sprintf("SKU-%03d", rand.IntN(500)), "qty": 1 + rand.IntN(3)}},
			"note":   note, "createdAt": time.Now().Add(-time.Duration(60-i) * time.Minute),
		})
		if err != nil {
			log.Fatal(err)
		}
		recs = append(recs, &kgo.Record{Topic: "order-avro", Key: fmt.Appendf(nil, "o-%d", 5000+i), Value: frame(orderID, v)})
	}

	// Protobuf: the same files serve the registry and the plain topic.
	comp := protocompile.Compiler{Resolver: protocompile.WithStandardImports(&protocompile.SourceResolver{ImportPaths: []string{protoRoot}})}
	files, err := comp.Compile(ctx, "acme/payments/payment.proto", "acme/logistics/shipment.proto")
	if err != nil {
		log.Fatal(err)
	}
	src := func(p string) string {
		b, err := protoFile(p)
		if err != nil {
			log.Fatal(err)
		}
		return b
	}
	register("acme/common/money.proto", "PROTOBUF", src("acme/common/money.proto"), nil)
	payID := register("payment-proto-value", "PROTOBUF", src("acme/payments/payment.proto"),
		[]ref{{Name: "acme/common/money.proto", Subject: "acme/common/money.proto", Version: 1}})
	pay := files[0].Messages().ByName("PaymentEvent")
	for i := 1; i <= 40; i++ {
		m := dynamicpb.NewMessage(pay)
		set(m, "payment_id", fmt.Sprintf("pay-%d", 9000+i))
		set(m, "order_id", fmt.Sprintf("o-%d", 5000+i))
		money := dynamicpb.NewMessage(pay.Fields().ByName("amount").Message())
		set(money, "currency", "CNY")
		set(money, "cents", int64(100+rand.IntN(99900)))
		m.Set(pay.Fields().ByName("amount"), protoreflect.ValueOfMessage(money))
		m.Set(pay.Fields().ByName("status"), protoreflect.ValueOfEnum(protoreflect.EnumNumber(1+i%3)))
		at := m.Mutable(pay.Fields().ByName("at")).Message()
		at.Set(at.Descriptor().Fields().ByName("seconds"), protoreflect.ValueOfInt64(time.Now().Add(-time.Duration(40-i)*time.Minute).Unix()))
		attrs := m.Mutable(pay.Fields().ByName("attributes")).Map()
		attrs.Set(protoreflect.ValueOfString("channel").MapKey(), protoreflect.ValueOfString([]string{"app", "web"}[i%2]))
		b, err := proto.Marshal(m)
		if err != nil {
			log.Fatal(err)
		}
		recs = append(recs, &kgo.Record{Topic: "payment-proto", Key: fmt.Appendf(nil, "pay-%d", 9000+i), Value: frame(payID, b, 0)})
	}

	ship := files[1].Messages().ByName("Shipment")
	for i := 1; i <= 30; i++ {
		m := dynamicpb.NewMessage(ship)
		set(m, "shipment_id", fmt.Sprintf("sh-%d", 700+i))
		set(m, "order_id", fmt.Sprintf("o-%d", 5000+i))
		set(m, "carrier", []string{"SF", "JD", "ZTO"}[i%3])
		set(m, "tracking_seq", int64(1850000000000000000)+int64(i)*7919)
		cps := m.Mutable(ship.Fields().ByName("checkpoints")).List()
		for _, city := range []string{"Shenzhen", "Hangzhou", "Shanghai"}[:1+i%3] {
			cp := cps.NewElement().Message()
			cp.Set(cp.Descriptor().Fields().ByName("city"), protoreflect.ValueOfString(city))
			cp.Set(cp.Descriptor().Fields().ByName("at_unix"), protoreflect.ValueOfInt64(time.Now().Unix()-int64(rand.IntN(86400))))
			cps.Append(protoreflect.ValueOfMessage(cp))
		}
		b, err := proto.Marshal(m)
		if err != nil {
			log.Fatal(err)
		}
		recs = append(recs, &kgo.Record{Topic: "shipment-pb", Key: fmt.Appendf(nil, "sh-%d", 700+i), Value: b})
	}
	if err := cl.ProduceSync(ctx, recs...).FirstErr(); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("produced %d messages to order-avro, payment-proto, shipment-pb\n", len(recs))
}

func set(m *dynamicpb.Message, field string, v any) {
	m.Set(m.Descriptor().Fields().ByName(protoreflect.Name(field)), protoreflect.ValueOf(v))
}

func protoFile(p string) (string, error) {
	b, err := os.ReadFile(filepath.Join(protoRoot, p))
	return string(b), err
}
