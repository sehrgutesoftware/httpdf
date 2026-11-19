package template

import (
	"html/template"
	"path"

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

// chunk takes an array and returns an array of arrays with n elements each.
func chunk(args ...any) any {
	if len(args) < 2 {
		return nil
	}

	arr := args[0].([]any)
	n := args[1].(int)

	chunks := make([][]any, 0)
	for i := 0; i < len(arr); i += n {
		end := min(i+n, len(arr))
		chunks = append(chunks, arr[i:end])
	}

	return chunks
}

func assetsFunc(assetsPrefix string) func(...string) string {
	return func(p ...string) string {
		return path.Join(append([]string{assetsPrefix}, p...)...)
	}
}
