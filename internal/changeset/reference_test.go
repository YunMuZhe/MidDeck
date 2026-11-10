package changeset

import (
	"strings"
	"testing"
)

// The example handed to AI tools must itself be a valid changeset.
func TestReferenceExampleParses(t *testing.T) {
	start := strings.Index(Reference, "version: 1")
	end := strings.Index(Reference, "Rules:")
	if start < 0 || end < start {
		t.Fatal("reference lost its example")
	}
	s, err := Parse([]byte(Reference[start:end]))
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Kafka.Topics) != 2 || len(s.RabbitMQ.Queues) != 1 || len(s.Envs) != 1 {
		t.Fatalf("unexpected example: %+v", s)
	}
}
