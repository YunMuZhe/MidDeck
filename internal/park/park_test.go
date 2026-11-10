package park

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/YunMuZhe/MidDeck/internal/driver/kafka"
)

func TestSaveListRecords(t *testing.T) {
	dir := t.TempDir()
	s := Open(dir)
	bin := []byte{0xff, 0x00, 0xfe, 'x'}
	recs := []kafka.Record{
		{Partition: 0, Offset: 41, Timestamp: 1, Key: kafka.Bytes("k1"), Value: kafka.Bytes(`{"orderId":1042}`),
			Headers: []kafka.RecordHeader{{Key: "trace", Value: kafka.Bytes("abc")}}},
		{Partition: 2, Offset: 7, Value: kafka.Bytes(bin)}, // binary value, nil key
	}
	b := &Batch{ID: NewID(time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC), "ab12cd"), InstanceID: "i1", Group: "g", Topic: "t"}
	if err := s.Save(b, recs); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(b, recs); err == nil {
		t.Fatal("saving the same batch twice must fail rather than overwrite")
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "parked", b.ID, "messages.jsonl"))
	if !strings.Contains(string(raw), `"value":"{\"orderId\":1042}"`) || !strings.Contains(string(raw), `"base64"`) {
		t.Fatalf("text should stay readable and binary should be base64:\n%s", raw)
	}
	got, err := s.Records(b.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || !bytes.Equal(got[1].Value, bin) || got[1].Key != nil || string(got[0].Headers[0].Value) != "abc" {
		t.Fatalf("round trip: %+v", got)
	}
	list, _ := s.List("i1")
	if len(list) != 1 || list[0].Count != 2 || list[0].Bytes != int64(2+16+4) {
		t.Fatalf("list %+v", list)
	}
	if l, _ := s.List("other"); len(l) != 0 {
		t.Fatal("filter by instance")
	}
	if _, err := s.Get("../../etc"); err == nil {
		t.Fatal("ids must not escape the directory")
	}
	if err := s.Delete(b.ID); err != nil {
		t.Fatal(err)
	}
	if l, _ := s.List(""); len(l) != 0 {
		t.Fatal("deleted")
	}
}
