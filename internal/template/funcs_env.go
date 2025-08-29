package template

import (
	"os"
	"text/template"
)

func envTemplateFuncs(funcs template.FuncMap, exposedEnvVars []string) {
	funcs["env"] = envFunc(exposedEnvVars)
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
