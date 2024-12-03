package template

import "text/template"

var templateFuncs = template.FuncMap{
	"chunk": chunk,
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
		end := i + n
		if end > len(arr) {
			end = len(arr)
		}
		chunks = append(chunks, arr[i:end])
	}

	return chunks
}
