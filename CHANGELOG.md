# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- This CHANGELOG.md
- `safeHTML`, `safeURL` and `safeCSS` template functions, which insert trusted values without escaping.

### Changed

- **Breaking:** Templates are rendered with Go's `html/template` instead of `text/template`, as the documentation always stated. Values are escaped according to the context they're inserted into (HTML text, attributes, URLs, CSS or JavaScript), and HTML comments are removed from the output. Check your templates for the following:
  - Values containing markup are escaped, e.g. translations with HTML tags. Mark trusted ones with `safeHTML`: `{{ tr "terms" | safeHTML }}`.
  - Values that are unsafe in their context are replaced with `ZgotmplZ`, e.g. `data:` URLs from the request data in a `src` attribute, or CSS values like `rgb(0, 0, 0)` in a `style` attribute. Mark trusted ones with `safeURL` or `safeCSS`.
  - In `<script>` elements, values are encoded as JavaScript, so `var items = {{ .items | toJson }};` turns the items into a string. Use `var items = {{ .items }};` instead.

  `httpdf-server validate` helps with the migration: it reports values replaced with `ZgotmplZ` when rendering the example data, and template actions in ambiguous contexts, e.g. inside an unclosed attribute.

### Removed

- **Breaking:** The Sprig functions `expandenv` and `getHostByName`. Use `env` with `exposedEnvVars` instead of `expandenv`.

### Fixed

- `chunk` with a size of 0 looped forever, until the server ran out of memory. It now fails for sizes below 1.
- `chunk` failed for sizes from the request data, which are JSON numbers, and for lists other than JSON arrays, e.g. from Sprig's `splitList`.
- **Breaking:** `generateTaggedPDF: false` had no effect, because Chromium tags PDFs by default. Templates without the option now produce untagged PDFs. Set `generateTaggedPDF: true` to keep them tagged.
- Each render left a zombie process behind, which piled up until the container was restarted, because httpdf runs as PID 1 and doesn't reap orphaned processes. The image now runs httpdf through [tini](https://github.com/krallin/tini). If you override the entrypoint, e.g. with Kubernetes' `command`, keep `/sbin/tini --` in front of `httpdf-server`.
- Each render left its browser profile of about 1 MB in the container's temp directory.

### Security

- Request data is escaped. Before, it was inserted into the HTML verbatim, so anyone who controlled a value could inject markup and scripts into the page Chromium renders, e.g. to load internal URLs into the PDF.
- Templates can no longer read environment variables that aren't listed in `exposedEnvVars`, which was possible via Sprig's `expandenv`, or make DNS lookups via `getHostByName`.

## [0.10.1] - 2026-11-19

### Added

- The server validates all templates on startup and logs a warning for each invalid one.

## [0.10.0] - 2026-11-19

### Added

- Templates can be split into multiple files: every `.html` file in the optional `partials/` directory can be included by its path, e.g. `{{ template "header.html" . }}`.
- `httpdf-server validate [template ...]` checks templates for errors.

### Changed

- Template errors refer to the main template as `template.html` instead of `main`.

### Fixed

- A `locale` config without a usable locale, e.g. `locale: {}`, caused a panic when loading the template. It's now reported as a config error.

## [0.9.1] - 2026-11-04

### Added

- PDFs carry the httpdf version in their Creator metadata, e.g. `httpdf/0.9.1 Chromium`.

## [0.9.0] - 2026-11-04

### Added

- On `SIGTERM` or `SIGINT`, the server stops accepting connections and gives in-flight requests 25 seconds to complete before shutting down. A second signal terminates it immediately.

### Changed

- The Docker image ships Chromium's headless shell instead of the full browser and no longer includes Mesa's GPU driver and LLVM, which saves about 200 MB.
- The browser binary moved from `/usr/bin/chromium` to `/usr/bin/chromium-headless-shell`.

## [0.8.0] - 2026-11-04

### Added

- Structured logging, configured via `HTTPDF_LOG_FORMAT` (`json` or `text`) and `HTTPDF_LOG_LEVEL` (`trace`, `debug`, `info`, `warn` or `error`)
- Each request is logged with a request ID, taken from the `X-Request-ID` header or generated, and returned in the response. The trace ID of a W3C `traceparent` header is logged as well.

### Changed

- Logs are written to stdout as JSON by default.
- Chromium's output is discarded unless `HTTPDF_LOG_LEVEL` is `trace`.
- Successful asset requests are logged at `debug` instead of `info`.
- The Docker image is based on Alpine 3.24 and built with Go 1.27.3.

### Fixed

- Data that doesn't match the template's schema got a `500` response instead of `422`.
- Template rendering errors resulted in a PDF of the error message instead of a `500` response.
- Concurrent first requests for the same template could crash the server.
- A template without an `assets/` directory that referenced an asset caused a panic while rendering.

## [0.7.3] - 2026-10-23

### Added

- `GET /healthz` endpoint for Kubernetes liveness and readiness probes.

## [0.7.2] - 2025-10-07

### Added

- `qrCode` template function, which returns a QR code as a PNG data URL.

## [0.7.1] - 2025-09-03

### Added

- The Go client's `Render` method accepts an optional language, which is sent as the `lang` query parameter.

## [0.7.0] - 2025-08-29

### Added

- `trLocale` template function to translate a key into a specific locale.

### Changed

- **Breaking:** The `{{ .__assets__ }}` and `{{ .__locale__ }}` template variables were replaced by the `asset` and `locale` functions. Update your templates as follows:

  ```html
  <!-- Before -->
  <link rel="stylesheet" href="{{ .__assets__ }}/style.css">
  <img src="{{ .__assets__ }}/images/logo.png">
  <p>Current locale: {{ .__locale__ }}</p>

  <!-- After -->
  <link rel="stylesheet" href="{{ asset "style.css" }}">
  <img src="{{ asset "images" "logo.png" }}">
  <p>Current locale: {{ locale }}</p>
  ```

[Unreleased]: https://github.com/sehrgutesoftware/httpdf/compare/v0.10.1...HEAD
[0.10.1]: https://github.com/sehrgutesoftware/httpdf/compare/v0.10.0...v0.10.1
[0.10.0]: https://github.com/sehrgutesoftware/httpdf/compare/v0.9.1...v0.10.0
[0.9.1]: https://github.com/sehrgutesoftware/httpdf/compare/v0.9.0...v0.9.1
[0.9.0]: https://github.com/sehrgutesoftware/httpdf/compare/v0.8.0...v0.9.0
[0.8.0]: https://github.com/sehrgutesoftware/httpdf/compare/v0.7.3...v0.8.0
[0.7.3]: https://github.com/sehrgutesoftware/httpdf/compare/v0.7.2...v0.7.3
[0.7.2]: https://github.com/sehrgutesoftware/httpdf/compare/v0.7.1...v0.7.2
[0.7.1]: https://github.com/sehrgutesoftware/httpdf/compare/v0.7.0...v0.7.1
[0.7.0]: https://github.com/sehrgutesoftware/httpdf/compare/v0.6.2...v0.7.0
