package changeset

import (
	"errors"
	"testing"
)

func TestLibrary(t *testing.T) {
	l := Library{Dir: t.TempDir()}
	if _, err := CleanName("../evil"); err == nil {
		t.Fatal("path escape accepted")
	}
	if n, err := CleanName("orders"); err != nil || n != "orders.yaml" {
		t.Fatal(n, err)
	}
	if _, err := l.Write("bad", "not: [yaml", false); err == nil {
		t.Fatal("invalid YAML accepted")
	}
	y := "version: 1\nkafka:\n  topics:\n    - name: a\n      partitions: 1\n"
	if _, err := l.Write("orders", y, false); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Write("orders.yaml", y, false); !errors.Is(err, ErrExists) {
		t.Fatalf("overwrite without asking: %v", err)
	}
	fs, err := l.List()
	if err != nil || len(fs) != 1 || fs[0].Name != "orders.yaml" {
		t.Fatalf("%+v %v", fs, err)
	}
	if got, _ := l.Read("orders"); got != y {
		t.Fatalf("read %q", got)
	}
	if err := l.Delete("orders"); err != nil {
		t.Fatal(err)
	}
}
