package cli

import (
	"encoding"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// TestVMDocument_schema holds jsonAPIVersion to its promise: within a version,
// fields may only be added. The shape of vmDocument is recorded in
// testdata/schema_<version>.txt, one field to a line. A recorded line that no
// longer matches means a field was removed, renamed or changed type, which
// breaks scripts, so the test fails until jsonAPIVersion is bumped; -update
// does not get round that. New fields are recorded with -update.
func TestVMDocument_schema(t *testing.T) {
	t.Parallel()

	got := jsonSchema(t, reflect.TypeFor[vmDocument]())
	path := filepath.Join("testdata", "schema_"+strings.ReplaceAll(jsonAPIVersion, "/", "_")+".txt")

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) && *update {
		// A new version starts a new record.
		writeSchema(t, path, got)

		return
	}

	if err != nil {
		t.Fatalf("reading %s (run with -update to create it): %v", path, err)
	}

	want := strings.Split(strings.TrimSpace(string(data)), "\n")

	var removed, added []string

	for _, line := range want {
		if !slices.Contains(got, line) {
			removed = append(removed, line)
		}
	}

	for _, line := range got {
		if !slices.Contains(want, line) {
			added = append(added, line)
		}
	}

	if len(removed) > 0 {
		t.Fatalf("breaking change to the JSON output of %s; bump jsonAPIVersion.\nNo longer matched:\n\t%s\nNew or changed:\n\t%s",
			jsonAPIVersion, strings.Join(removed, "\n\t"), strings.Join(added, "\n\t"))
	}

	if len(added) == 0 {
		return
	}

	if *update {
		writeSchema(t, path, got)

		return
	}

	t.Errorf("new fields in the JSON output; run with -update to record them in %s:\n\t%s",
		path, strings.Join(added, "\n\t"))
}

func writeSchema(t *testing.T, path string, lines []string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}

// jsonSchema lists every field that encoding/json writes for typ, sorted, as
// "path type", such as "results[].pricing.onDemandHourly number nullable". A
// pointer is nullable, as it can be written as null; an omitempty field is
// optional, as it can be left out.
func jsonSchema(t *testing.T, typ reflect.Type) []string {
	t.Helper()

	s := &schemaWalker{t: t}
	s.walk("", typ, "")
	slices.Sort(s.lines)

	return s.lines
}

type schemaWalker struct {
	t     *testing.T
	lines []string
}

var (
	jsonMarshaler = reflect.TypeFor[json.Marshaler]()
	textMarshaler = reflect.TypeFor[encoding.TextMarshaler]()
)

// walk records the value at path, of type typ, and anything inside it. mods
// are added to its own line.
func (s *schemaWalker) walk(path string, typ reflect.Type, mods string) {
	// omitempty leaves a nil pointer out rather than writing null.
	if typ.Kind() == reflect.Pointer {
		if mods == "" {
			mods = " nullable"
		}

		typ = typ.Elem()
	}

	for _, m := range []reflect.Type{jsonMarshaler, textMarshaler} {
		if typ.Implements(m) || reflect.PointerTo(typ).Implements(m) {
			s.t.Fatalf("%s: %s marshals itself, so its shape cannot be read; teach jsonSchema about it", path, typ)
		}
	}

	add := func(kind string) { s.lines = append(s.lines, path+" "+kind+mods) }

	switch typ.Kind() {
	case reflect.String:
		add("string")
	case reflect.Bool:
		add("boolean")
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		add("number")
	case reflect.Slice, reflect.Array:
		add("array")
		s.walk(path+"[]", typ.Elem(), "")
	case reflect.Struct:
		if path != "" {
			add("object")
		}

		s.fields(path, typ)
	default:
		s.t.Fatalf("%s: jsonSchema does not handle %s", path, typ)
	}
}

// fields walks the fields of the struct typ as encoding/json sees them:
// unexported and "-" fields are skipped, and untagged embedded structs are
// flattened into their parent.
func (s *schemaWalker) fields(path string, typ reflect.Type) {
	for f := range typ.Fields() {
		tag := f.Tag.Get("json")
		if tag == "-" || !f.IsExported() {
			continue
		}

		name, opts, _ := strings.Cut(tag, ",")

		if f.Anonymous && name == "" && f.Type.Kind() == reflect.Struct {
			s.fields(path, f.Type)

			continue
		}

		if name == "" {
			name = f.Name
		}

		if path != "" {
			name = path + "." + name
		}

		var mods string
		if slices.Contains(strings.Split(opts, ","), "omitempty") {
			mods = " optional"
		}

		s.walk(name, f.Type, mods)
	}
}
