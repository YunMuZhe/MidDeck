package decode

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bufbuild/protocompile"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/dynamicpb"
)

// protoFiles is a compiled set of .proto files.
type protoFiles struct {
	main  protoreflect.FileDescriptor // registry schemas only
	types *protoregistry.Types
}

func newProtoFiles(files []protoreflect.FileDescriptor) *protoFiles {
	pf := &protoFiles{types: &protoregistry.Types{}}
	seen := map[string]bool{}
	var addFile func(f protoreflect.FileDescriptor)
	var addMsgs func(ms protoreflect.MessageDescriptors)
	addMsgs = func(ms protoreflect.MessageDescriptors) {
		for i := 0; i < ms.Len(); i++ {
			m := ms.Get(i)
			_ = pf.types.RegisterMessage(dynamicpb.NewMessageType(m))
			addMsgs(m.Messages())
		}
	}
	addFile = func(f protoreflect.FileDescriptor) {
		if seen[f.Path()] {
			return
		}
		seen[f.Path()] = true
		addMsgs(f.Messages())
		for i := 0; i < f.Imports().Len(); i++ {
			addFile(f.Imports().Get(i).FileDescriptor)
		}
	}
	for _, f := range files {
		addFile(f)
	}
	return pf
}

// byIndexes walks a Confluent message-index path from the main file.
func (pf *protoFiles) byIndexes(idx []int) (protoreflect.MessageDescriptor, error) {
	ms := pf.main.Messages()
	var md protoreflect.MessageDescriptor
	for _, i := range idx {
		if i < 0 || i >= ms.Len() {
			return nil, fmt.Errorf("message index %v not found in schema", idx)
		}
		md = ms.Get(i)
		ms = md.Messages()
	}
	if md == nil {
		return nil, errors.New("schema has no message types")
	}
	return md, nil
}

func (pf *protoFiles) toJSON(md protoreflect.MessageDescriptor, b []byte) (string, error) {
	msg := dynamicpb.NewMessage(md)
	if err := (proto.UnmarshalOptions{Resolver: pf.types}).Unmarshal(b, msg); err != nil {
		return "", err
	}
	// A wrong type usually "decodes" into nothing but unknown fields.
	if len(msg.GetUnknown()) > 0 {
		known := false
		msg.Range(func(protoreflect.FieldDescriptor, protoreflect.Value) bool { known = true; return false })
		if !known {
			return "", fmt.Errorf("bytes do not match %s", md.FullName())
		}
	}
	out, err := protojson.MarshalOptions{UseProtoNames: true, Resolver: pf.types}.Marshal(msg)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer // protojson randomizes whitespace on purpose
	if err := json.Compact(&buf, out); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// Imports the Confluent serializers add without registering them as references.
var confluentProtos = map[string]string{
	"confluent/meta.proto": `syntax = "proto3";
package confluent;
import "google/protobuf/descriptor.proto";
message Meta { string doc = 1; map<string, string> params = 2; repeated string tags = 3; }
extend google.protobuf.FileOptions { Meta file_meta = 1088; }
extend google.protobuf.MessageOptions { Meta message_meta = 1088; }
extend google.protobuf.FieldOptions { Meta field_meta = 1088; }
extend google.protobuf.EnumOptions { Meta enum_meta = 1088; }
extend google.protobuf.EnumValueOptions { Meta enum_value_meta = 1088; }
`,
	"confluent/type/decimal.proto": `syntax = "proto3";
package confluent.type;
message Decimal { bytes value = 1; uint32 precision = 2; int32 scale = 3; }
`,
}

func compile(ctx context.Context, r protocompile.Resolver, names ...string) ([]protoreflect.FileDescriptor, error) {
	c := protocompile.Compiler{Resolver: protocompile.WithStandardImports(r)}
	files, err := c.Compile(ctx, names...)
	if err != nil {
		return nil, err
	}
	out := make([]protoreflect.FileDescriptor, len(files))
	for i, f := range files {
		out[i] = f
	}
	return out, nil
}

// protoForSchema compiles a registry Protobuf schema with its references.
func (d *Decoder) protoForSchema(ctx context.Context, s *Schema) (*protoFiles, error) {
	d.mu.Lock()
	pf, ok := d.pb[s.ID]
	d.mu.Unlock()
	if ok {
		return pf, nil
	}
	sources := map[string]string{}
	var load func(refs []Reference, depth int) error
	load = func(refs []Reference, depth int) error {
		if depth > 16 {
			return errors.New("schema references nested too deeply")
		}
		for _, ref := range refs {
			if _, ok := sources[ref.Name]; ok {
				continue
			}
			rs, err := d.reg.Version(ctx, ref.Subject, fmt.Sprint(ref.Version))
			if err != nil {
				return fmt.Errorf("reference %s: %w", ref.Name, err)
			}
			sources[ref.Name] = rs.Schema
			if err := load(rs.References, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	if err := load(s.References, 0); err != nil {
		return nil, err
	}
	main := fmt.Sprintf("middeck/registry/schema-%d.proto", s.ID)
	sources[main] = s.Schema
	res := &protocompile.SourceResolver{Accessor: func(p string) (io.ReadCloser, error) {
		if src, ok := sources[p]; ok {
			return io.NopCloser(strings.NewReader(src)), nil
		}
		if src, ok := confluentProtos[p]; ok {
			return io.NopCloser(strings.NewReader(src)), nil
		}
		return nil, fs.ErrNotExist
	}}
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	files, err := compile(cctx, res, main)
	if err != nil {
		return nil, err
	}
	pf = newProtoFiles(files)
	pf.main = files[0]
	d.mu.Lock()
	d.pb[s.ID] = pf
	d.mu.Unlock()
	return pf, nil
}

// localProtos compiles the user's .proto files lazily, once.
type localProtos struct {
	paths []string

	mu       sync.Mutex
	loaded   bool
	set      *protoFiles
	byName   map[string]protoreflect.MessageDescriptor
	names    []string
	problems []string
}

func newLocalProtos(paths []string) *localProtos { return &localProtos{paths: paths} }

func (l *localProtos) reset() {
	l.mu.Lock()
	l.loaded = false
	l.mu.Unlock()
}

func (l *localProtos) types() ([]string, []string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.load()
	return l.names, l.problems
}

func (l *localProtos) find(typ string) (*protoFiles, protoreflect.MessageDescriptor, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.load()
	md, ok := l.byName[strings.TrimPrefix(typ, ".")]
	if !ok {
		if len(l.paths) == 0 {
			return nil, nil, fmt.Errorf("protobuf type %s: no .proto files configured for this connection", typ)
		}
		return nil, nil, fmt.Errorf("protobuf type %s not found in the configured .proto files", typ)
	}
	return l.set, md, nil
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if h, err := os.UserHomeDir(); err == nil {
			return filepath.Join(h, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}

// load must be called with l.mu held.
func (l *localProtos) load() {
	if l.loaded {
		return
	}
	l.loaded = true
	l.set, l.byName, l.names, l.problems = nil, map[string]protoreflect.MessageDescriptor{}, nil, nil
	var roots, names []string
	for _, p := range l.paths {
		p = expandHome(strings.TrimSpace(p))
		if p == "" {
			continue
		}
		st, err := os.Stat(p)
		if err != nil {
			l.problems = append(l.problems, err.Error())
			continue
		}
		if !st.IsDir() {
			roots = append(roots, filepath.Dir(p))
			names = append(names, filepath.Base(p))
			continue
		}
		roots = append(roots, p)
		_ = filepath.WalkDir(p, func(f string, de fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if de.IsDir() && f != p && strings.HasPrefix(de.Name(), ".") {
				return filepath.SkipDir
			}
			if !de.IsDir() && strings.HasSuffix(f, ".proto") {
				rel, _ := filepath.Rel(p, f)
				names = append(names, filepath.ToSlash(rel))
			}
			return nil
		})
	}
	if len(names) == 0 {
		return
	}
	names = dedupe(names)
	res := &protocompile.SourceResolver{ImportPaths: roots}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	files, err := compile(ctx, res, names...)
	if err != nil {
		// Salvage what compiles on its own and report the rest.
		files = nil
		for _, n := range names {
			f, err := compile(ctx, res, n)
			if err != nil {
				if len(l.problems) < 20 {
					l.problems = append(l.problems, err.Error())
				}
				continue
			}
			files = append(files, f...)
		}
	}
	l.set = newProtoFiles(files)
	l.set.types.RangeMessages(func(mt protoreflect.MessageType) bool {
		md := mt.Descriptor()
		name := string(md.FullName())
		if strings.HasPrefix(name, "google.protobuf.") {
			return true
		}
		if _, ok := l.byName[name]; !ok && !md.IsMapEntry() {
			l.byName[name] = md
			l.names = append(l.names, name)
		}
		return true
	})
	sort.Strings(l.names)
}

func dedupe(s []string) []string {
	seen := map[string]bool{}
	out := s[:0]
	for _, x := range s {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}
