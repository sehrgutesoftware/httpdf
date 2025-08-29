package template

import (
	"text/template"

	"github.com/kaptinlin/go-i18n"
)

func i18nTemplateFuncs(funcs template.FuncMap, bundle *i18n.I18n, locale string) {
	var localizer *i18n.Localizer
	if bundle != nil {
		localizer = bundle.NewLocalizer(bundle.MatchAvailableLocale(locale))
		locale = localizer.Locale()
	}

	funcs["locale"] = localeFunc(locale)
	funcs["tr"] = translateFunc(localizer)
	funcs["trLocale"] = translateLocaleFunc(bundle)
}

func localeFunc(locale string) func() string {
	return func() string {
		return locale
	}
}

func translateFunc(localizer *i18n.Localizer) func(key string, args ...any) string {
	return func(key string, args ...any) string {
		if localizer == nil {
			return "!(localizer is nil)"
		}
		return localizer.Get(key, i18nVars(args))
	}
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

func translateLocaleFunc(bundle *i18n.I18n) func(locale, key string, args ...any) string {
	localizers := make(map[string]*i18n.Localizer)

	return func(locale, key string, args ...any) string {
		if bundle == nil {
			return "!(bundle is nil)"
		}

		localizer, exists := localizers[locale]
		if !exists {
			localizer = bundle.NewLocalizer(bundle.MatchAvailableLocale(locale))
			localizers[locale] = localizer
		}
		if localizer == nil {
			return "!(localizer is nil)"
		}

		return localizer.Get(key, i18nVars(args))
	}
}
