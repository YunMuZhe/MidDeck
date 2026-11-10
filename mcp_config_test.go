package main

import (
	"reflect"
	"testing"
)

func TestMergePatch(t *testing.T) {
	doc := map[string]any{"name": "a", "auth": map[string]any{"username": "u", "password": "p"}, "tags": []any{"x"}}
	merge(doc, map[string]any{"auth": map[string]any{"password": "q"}, "tags": []any{"y", "z"}, "name": nil})
	want := map[string]any{"auth": map[string]any{"username": "u", "password": "q"}, "tags": []any{"y", "z"}}
	if !reflect.DeepEqual(doc, want) {
		t.Fatalf("%v", doc)
	}
}
