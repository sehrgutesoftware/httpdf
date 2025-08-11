package template

import (
	"os"
	"text/template"

	"github.com/Masterminds/sprig/v3"
	"github.com/kaptinlin/go-i18n"
)

func templateFuncs(localizer *i18n.Localizer, exposedEnvVars []string) template.FuncMap {
	funcs := sprig.FuncMap()

	funcs["chunk"] = chunk
	funcs["tr"] = func(key string, args ...any) string {
		if localizer == nil {
			return "!(localizer is nil)"
		}
		return localizer.Get(key, i18nVars(args))
	}
	funcs["env"] = envFunc(exposedEnvVars)

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

func i18nVars(args []any) i18n.Vars {
	vars := make(i18n.Vars, len(args)/2)
	if len(args) == 0 {
		return vars
	}

	if len(args)%2 != 0 {
		args = append(args, nil) // Ensure even number of arguments
	}

	for i := 0; i < len(args); i += 2 {
		if key, ok := args[i].(string); ok {
			vars[key] = args[i+1]
		}
	}

	return vars
}

// envFunc returns a template function that provides access to environment variables
// only if they are in the exposedEnvVars whitelist.
func envFunc(exposedEnvVars []string) func(string) string {
	// Create a map for O(1) lookup of exposed variables
	exposed := make(map[string]bool, len(exposedEnvVars))
	for _, envVar := range exposedEnvVars {
		exposed[envVar] = true
	}

	return func(key string) string {
		if !exposed[key] {
			return ""
		}
		return os.Getenv(key)
	}
}
