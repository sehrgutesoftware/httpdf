# httpdf

PDF documents from HTML templates – as a web service

httpdf produces a PDF file from the following inputs:

1) an HTML template, written in go's [html/template](https://pkg.go.dev/html/template) syntax
2) a [JSON Schema](https://json-schema.org/) describing the inputs required to render the template
3) the actual template values as an HTTP POST request
4) an optional `?lang=` query parameter (or `Accept-Language` header) to select a translation file

It uses a headless Chromium or Firefox instance for PDF rendering, controlled via [rod](https://pkg.go.dev/github.com/go-rod/rod).

Templates and schemas are plain text files and therefore easily manageable. The web service can render any number of templates – chosen via a unique identifier in the URL path.

## Usage

The web service can be run as a simple go binary or as a [docker image](https://ghcr.io/sehrgutesoftware/httpdf).

### API

#### `POST /templates/{template}/render`
Render the template using the JSON-encoded data provided in the request body. Successful response is of `Content-Type: application/pdf`. Templates are loaded on first use and cached in memory. In order to reload a template, the server must be restarted.

#### `GET /templates/{template}/preview`
Render an HTML preview of the template using data from the template's `example.json` file. Useful for template development. The preview endpoint reads the template from disk on each request, so you can test changes without restarting the server.

### Template Development

> Check the [templates/example](./templates/example/) directory for an example template.

Templates are written in go's [html/template](https://pkg.go.dev/html/template) syntax and can include any valid HTML. The template must be accompanied by a JSON Schema that describes the data structure required to render the template, as well as a configuration file defining the page layout.

#### Template Structure

Mount your templates into the container under the `/templates` directory. Each template is a folder by itself. The structure of the folder is as follows (file names must match exactly):

```
/templates
└── /example                # folder name = template name
    ├── template.html       # the HTML template itself
    ├── schema.json         # JSON Schema describing the data structure required by the template
    ├── config.yaml         # template config parameters
    ├── example.json        # (optional) example values
    ├── /assets             # (optional) static assets
    └── /locales/*.yaml     # (optional) translation files
```

The template is identified by its folder name. In the above example, the name of the template is `example`.

`template.html` itself must contain valid (= renderable by Chromium) HTML, using [html/template](https://pkg.go.dev/html/template) as a templating language.

`schema.json` must be a valid JSON Schema according to [Draft 2020-12](https://json-schema.org/draft/2020-12). Its purpose is to validate the input data before populating the HTML template. Though not recommended, the schema can be empty (define an object with no properties). If your template is using placeholders that are not defined in the schema, you risk getting unclear errors during template rendering.

`config.yaml` contains configuration values related to the template. It has the following structure:

```yaml
page:
    width: width of the resulting PDF in mm
    height: height of the resulting PDF in mm

locale: # optional
    locales:
    - en
    - de
    default: en

exposedEnvVars: # optional; list of env vars available in the template
    - IMAGE_PROXY_URL
    - LANG
```

`example.json` can be added for testing and documentation purposes, providing some example data to render the template during template development.

`assets/` is an optional directory for static assets, such as images or stylesheets. They can be referenced in the HTML template using the `asset` function, e.g. `<link rel="stylesheet" href="{{ asset "style.css" }}">` or `<img src="{{ asset "images" "logo.png" }}">`.

`locales/` is an optional directory for translation files. Each file must be a valid YAML file containing key-value pairs for translations. The file names must match the language codes, e.g. `en.yaml`, `de.yaml`, etc. The translations can be accessed in the HTML template using the `tr` function, e.g. `{{ tr "key" }}`. The `tr` function will accept placeholders in the translation strings. The current locale can be accessed using `{{ locale }}`. See the [example](templates/example) for usage.

Only translation files for the languages defined in the `config.yaml` file will be loaded.


#### Template Functions

**Base Functions (always available):**
- All [Go template functions](https://pkg.go.dev/text/template#hdr-Functions) for basic operations
- All [Sprig](https://masterminds.github.io/sprig/) functions for general template operations
- `asset`: Generate asset URLs with the configured assets prefix. Usage: `{{ asset "style.css" }}` or `{{ asset "images" "logo.png" }}`
- `chunk`: Takes an array and returns an array of arrays with n elements each. Usage: `{{ chunk .items 2 }}`

**Internationalization Functions (when locale config is present):**
- `locale`: Returns the current locale string. Usage: `{{ locale }}`
- `tr`: Translation function for internationalized templates. Usage: `{{ tr "key" "arg1" "value1" }}`
- `trLocale`: Translation function with explicit locale. Usage: `{{ trLocale "de" "key" "arg1" "value1" }}`

**Environment Variable Functions (when exposedEnvVars is configured):**
- `env`: Access environment variables (security-controlled). Usage: `{{ env "VAR_NAME" }}`

##### Asset Management (`asset` function)

The `asset` function generates URLs for static assets with the configured assets prefix. This function takes one or more path segments and joins them with the assets prefix.

Example usage in template:
```html
<link rel="stylesheet" href="{{ asset "style.css" }}">
<img src="{{ asset "images" "logo.png" }}" alt="Logo">
<script src="{{ asset "js" "lib" "jquery.js" }}"></script>
```

##### Internationalization Functions

- `locale`: Returns the current locale string (e.g., "en", "de")
- `tr`: Translates a key using the current locale with optional parameters
- `trLocale`: Translates a key using a specific locale with optional parameters

Example usage:
```html
<p>Current language: {{ locale }}</p>
<h1>{{ tr "welcome_message" "name" .userName }}</h1>
<p>German greeting: {{ trLocale "de" "hello" }}</p>
```

##### Environment Variables (`env` function)

The `env` function provides controlled access to environment variables within templates. For security reasons, only environment variables explicitly listed in the template's `exposedEnvVars` configuration can be accessed.

Example usage in template:
```html
<p>Image proxy URL: {{ env "IMAGE_PROXY_URL" }}</p>
<p>Language: {{ env "LANG" }}</p>
```

If an environment variable is not in the exposed list or isn't set, the function returns an empty string. You can use the `default` function to provide fallback values.

## Migration Guide

### Breaking Changes in v0.7
The `{{ .__assets__ }}` and `{{ .__locale__ }}` template variables have been replaced with the `asset` and `locale` functions. If you're upgrading from a previous version of httpdf, you may need to update your templates to use the new function-based API:

### Asset references
```html
<!-- Old API -->
<link rel="stylesheet" href="{{ .__assets__ }}/style.css">
<img src="{{ .__assets__ }}/images/logo.png">

<!-- New API -->
<link rel="stylesheet" href="{{ asset "style.css" }}">
<img src="{{ asset "images" "logo.png" }}">
```

### Locale access
```html
<!-- Old API -->
<p>Current locale: {{ .__locale__ }}</p>

<!-- New API -->
<p>Current locale: {{ locale }}</p>
```

The new API provides better flexibility for asset path construction and clearer function semantics. All other template functions remain unchanged.

## Development

In order to start the development server, employing hot-reload, use docker compose:

```sh
docker compose up
```

The container image includes a Chromium instance for HTML rendering. If you want to run the server directly in your host OS without using docker, you need to tell the app the path to a Chromium binary (TBD).

```sh
go run ./cmd/server
```
