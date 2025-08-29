package template

import (
	"path"
	"text/template"

	"github.com/Masterminds/sprig/v3"
)

func templateFuncs(assetsPrefix string) template.FuncMap {
	funcs := sprig.FuncMap()
	funcs["chunk"] = chunk
	funcs["asset"] = assetsFunc(assetsPrefix)
	return funcs
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
