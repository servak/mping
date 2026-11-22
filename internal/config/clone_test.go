package config

import (
	"reflect"
	"testing"

	"github.com/servak/mping/internal/prober"
)

// A clone equals the original and shares no pointer, map or slice with it.
// Every field of every config type is filled in by reflection, so a new
// field that Clone does not copy fails the test.
func TestCloneSharesNothing(t *testing.T) {
	var c Config
	fill(t, reflect.ValueOf(&c).Elem(), "Config")

	clone := c.Clone()
	if !reflect.DeepEqual(&c, clone) {
		t.Fatalf("clone differs:\n%s\nwant:\n%s", Marshal(clone), Marshal(&c))
	}
	orig := make(map[uintptr]string)
	walkRefs(reflect.ValueOf(&c), "Config", func(path string, p uintptr) { orig[p] = path })
	walkRefs(reflect.ValueOf(clone), "Config", func(path string, p uintptr) {
		if shared, ok := orig[p]; ok {
			t.Errorf("%s of the clone is shared with the original (%s)", path, shared)
		}
	})
}

// Unset fields stay unset.
func TestCloneKeepsNil(t *testing.T) {
	for _, c := range []*Config{
		{},
		{Prober: map[string]*prober.ProberConfig{
			"nil":   nil,
			"empty": {},
			"http":  {HTTP: &prober.HTTPConfig{}},
			"tls":   {HTTP: &prober.HTTPConfig{TLS: &prober.TLSConfig{}}},
		}},
		DefaultConfig(),
	} {
		if clone := c.Clone(); !reflect.DeepEqual(c, clone) {
			t.Errorf("clone differs:\n%s\nwant:\n%s", Marshal(clone), Marshal(c))
		}
	}
}

// fill sets v and everything below it to non-zero values. It fails on kinds
// that it, or a copy, cannot handle (interfaces, channels, unexported
// fields, ...): those need a decision on how to copy them.
func fill(t *testing.T, v reflect.Value, path string) {
	t.Helper()
	if !v.CanSet() {
		t.Fatalf("%s: cannot be set (unexported field?)", path)
	}
	switch v.Kind() {
	case reflect.Bool:
		v.SetBool(true)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(7)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v.SetUint(7)
	case reflect.Float32, reflect.Float64:
		v.SetFloat(7)
	case reflect.String:
		v.SetString("x")
	case reflect.Pointer:
		v.Set(reflect.New(v.Type().Elem()))
		fill(t, v.Elem(), path)
	case reflect.Struct:
		for i := range v.NumField() {
			fill(t, v.Field(i), path+"."+v.Type().Field(i).Name)
		}
	case reflect.Array:
		for i := range v.Len() {
			fill(t, v.Index(i), path+"[]")
		}
	case reflect.Slice:
		v.Set(reflect.MakeSlice(v.Type(), 1, 1))
		fill(t, v.Index(0), path+"[]")
	case reflect.Map:
		key := reflect.New(v.Type().Key()).Elem()
		fill(t, key, path+"[key]")
		elem := reflect.New(v.Type().Elem()).Elem()
		fill(t, elem, path+"[]")
		v.Set(reflect.MakeMap(v.Type()))
		v.SetMapIndex(key, elem)
	default:
		t.Fatalf("%s: do not know how to fill a %s", path, v.Type())
	}
}

// walkRefs calls f for every pointer, map and slice at or below v, whose
// kinds are those of fill.
func walkRefs(v reflect.Value, path string, f func(path string, p uintptr)) {
	switch v.Kind() {
	case reflect.Pointer:
		if !v.IsNil() {
			f(path, v.Pointer())
			walkRefs(v.Elem(), path, f)
		}
	case reflect.Struct:
		for i := range v.NumField() {
			walkRefs(v.Field(i), path+"."+v.Type().Field(i).Name, f)
		}
	case reflect.Map:
		f(path, v.Pointer())
		for it := v.MapRange(); it.Next(); {
			walkRefs(it.Key(), path+"[key]", f)
			walkRefs(it.Value(), path+"[]", f)
		}
	case reflect.Slice:
		f(path, v.Pointer())
		fallthrough
	case reflect.Array:
		for i := range v.Len() {
			walkRefs(v.Index(i), path+"[]", f)
		}
	}
}
