# httpdf

PDF documents from HTML templates – as a web service (TBD: or command line utility).

httpdf produces a PDF file from the following inputs:

1) an HTML template, written in go's [html/template](https://pkg.go.dev/html/template) syntax
2) a [JSON Schema](https://json-schema.org/) describing the inputs required to render the template
3) the actual template values as an HTTP POST request (TBD: or as a plain JSON file)

It uses a headless Chromium or Firefox instance for PDF rendering, controlled via [rod](https://pkg.go.dev/github.com/go-rod/rod).

Templates and schemas are plain text files and therefore easily manageable. The web service can render any number of templates – chosen via a unique identifier in the URL path.

## Usage

The web service can be run as a simple go binary or as a [docker image](https://hub.docker.com/r/sehrgutesoftware/httpdf).

### Creating Templates

> Check the [templates/example](./templates/example/) directory for an example template.

Mount your templates into the container under the `/templates` directory. Each template is a folder by itself. The structure of the folder is as follows (file names must match exactly):

```
templates
└── example                 # folder name = template name
    ├── template.html       # the HTML template itself
    ├── schema.json         # JSON Schema describing the data structure required by the template
    ├── config.yaml         # template config parameters
    └── example.json        # (optional) example values
```

The template is identified by its folder name. In the above example, the name of the template is `example`.

`template.html` itself must contain valid (= renderable by Chromium) HTML, using [html/template](https://pkg.go.dev/html/template) as a templating language.

`schema.json` must be a valid JSON Schema according to [Draft 2020-12](https://json-schema.org/draft/2020-12). Its purpose is to validate the input data before populating the HTML template. Though not recommended, the schema can be empty (define an object with no properties). If your template is using placeholders that are not defined in the schema, you risk getting unclear errors during template rendering.

`config.yaml` contains configuration values related to the template. It has the following structure:

```yaml
page:
    width: width of the resulting PDF in mm
    height: height of the resulting PDF in mm
```

`example.json` is currently unused, but can be added for testing and documentation purposes, providing some example data to render the template. We might add some tooling to preview a template during template development using the provided sample data. We might also add an API endpoint to serve the example data to clients.

## Development

In order to start the development server, employing hot-reload, use docker compose:

```sh
docker compose up
```

The container image includes a Chromium instance for HTML rendering. If you want to run the server directly in your host OS without using docker, you need to tell the app the path to a Chromium binary (TBD).
