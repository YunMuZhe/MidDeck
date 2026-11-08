package compat

import "testing"

func TestCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"3.9.0", "3.9", 0},
		{"2.0.1", "2.1", -1},
		{"4.0.0-IV1", "3.9", 1},
		{"7.2.4", "7.2.4", 0},
		{"v3.13.7", "3.8", 1},
	}
	for _, c := range cases {
		if got := Compare(c.a, c.b); got != c.want {
			t.Errorf("Compare(%q,%q)=%d want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestCheck(t *testing.T) {
	if r := Check("kafka", "3.9.0"); r.Level != Tested {
		t.Errorf("kafka 3.9 level=%s", r.Level)
	}
	if r := Check("kafka", "2.2"); r.Level != Supported || len(r.Unavailable) == 0 {
		t.Errorf("kafka 2.2 got %+v", r)
	}
	if r := Check("redis", "4.0.14"); r.Level != Partial {
		t.Errorf("redis 4.0 level=%s", r.Level)
	}
	if r := Check("rabbitmq", "3.5.7"); r.Level != Unsupported {
		t.Errorf("rabbitmq 3.5 level=%s", r.Level)
	}
	if r := Check("redis", ""); r.Level != Unknown {
		t.Errorf("empty version level=%s", r.Level)
	}
}
