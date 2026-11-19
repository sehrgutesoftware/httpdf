package template

import (
	"fmt"
	"html/template"
	"math"
	"path"
	"reflect"

	"github.com/Masterminds/sprig/v3"
)

// unsafeSprigFuncs are removed from the template functions: env and expandenv
// can read any environment variable (env is replaced by a restricted version,
// see envTemplateFuncs), and getHostByName performs DNS lookups.
var unsafeSprigFuncs = []string{"env", "expandenv", "getHostByName"}

func templateFuncs(assetsPrefix string) template.FuncMap {
	funcs := sprig.FuncMap()
	for _, name := range unsafeSprigFuncs {
		delete(funcs, name)
	}
	funcs["chunk"] = chunk
	funcs["asset"] = assetsFunc(assetsPrefix)
	safeTemplateFuncs(funcs)
	return funcs
}

// safeTemplateFuncs adds functions that mark values as trusted, so html/template
// inserts them without escaping. They must not be used on untrusted data.
func safeTemplateFuncs(funcs template.FuncMap) {
	funcs["safeHTML"] = func(s string) template.HTML { return template.HTML(s) }
	funcs["safeURL"] = func(s string) template.URL { return template.URL(s) }
	funcs["safeCSS"] = func(s string) template.CSS { return template.CSS(s) }
}

// chunk takes a list and returns a list of lists with n elements each.
func chunk(args ...any) (any, error) {
	if len(args) < 2 {
		return nil, nil
	}

	list, err := toList(args[0])
	if err != nil {
		return nil, fmt.Errorf("chunk: %w", err)
	}
	n, err := toInt(args[1])
	if err != nil {
		return nil, fmt.Errorf("chunk: size: %w", err)
	}
	if n <= 0 {
		return nil, fmt.Errorf("chunk: size must be greater than 0, got %d", n)
	}

	chunks := make([][]any, 0)
	for i := 0; i < len(list); i += n {
		end := min(i+n, len(list))
		chunks = append(chunks, list[i:end])
	}

	return chunks, nil
}

// toList converts a slice or array of any element type to []any. nil is
// treated as an empty list, like range does.
func toList(v any) ([]any, error) {
	if v == nil {
		return []any{}, nil
	}
	if list, ok := v.([]any); ok {
		return list, nil
	}

	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
		return nil, fmt.Errorf("expected a list, got %T", v)
	}
	list := make([]any, rv.Len())
	for i := range list {
		list[i] = rv.Index(i).Interface()
	}
	return list, nil
}

// toInt converts an integer of any type, or a float without fractional part,
// such as a number from JSON, to int.
func toInt(v any) (int, error) {
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return int(rv.Int()), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return int(rv.Uint()), nil
	case reflect.Float32, reflect.Float64:
		// Converting floats outside of int's range is platform-dependent
		if f := rv.Float(); f == math.Trunc(f) && f >= math.MinInt && f < math.MaxInt {
			return int(f), nil
		}
		return 0, fmt.Errorf("expected an integer, got %v", v)
	default:
		return 0, fmt.Errorf("expected an integer, got %T", v)
	}
}

func assetsFunc(assetsPrefix string) func(...string) string {
	return func(p ...string) string {
		return path.Join(append([]string{assetsPrefix}, p...)...)
	}
}
