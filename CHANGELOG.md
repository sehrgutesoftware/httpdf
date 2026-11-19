# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- This CHANGELOG.md

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
