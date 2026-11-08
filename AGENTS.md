# AGENTS.md

## Project
Go service that scrapes the Cumbuco OceanView weather station (`clientraw.txt`,
with a fixed-layout GIF OCR fallback) and exports OpenTelemetry metrics to an
existing OTLP collector.

## Dev environment tips
- Requires Go 1.24+ (stdlib + OpenTelemetry Go SDK, no CGO).
- `go mod download` before building.
- `go run ./cmd/weather-otel -help` to see flags; collector connection is
  configured via `OTEL_EXPORTER_OTLP_*` environment variables.
- Keep generated context artefacts under `.context/`.

## Testing instructions
- `go test ./...` for the unit and golden suites.
- `go test -race ./...` before handing off concurrent changes.
- `go vet ./...` and `gofmt -l .` must be clean.
- Parser/OCR changes require a golden fixture in `internal/*/testdata/`.
- Never call the live station from tests; use `httptest.Server`.

## PR instructions
- Follow Conventional Commits (`feat:`, `fix:`, `docs:`, `test:`).
- Update `README.md` for any new flag or environment variable.
- Update the metric catalog in `.context/docs/data-flow.md` when metrics change.
- Paste a collector `debug` exporter sample showing the new metrics.

## Repository map
- `cmd/weather-otel/` — binary, configuration, lifecycle.
- `cmd/atlasgen/` — developer tool that regenerates the OCR glyph atlas.
- `internal/clientraw/` — Weather Display text parser.
- `internal/gifocr/` — fixed-layout OCR fallback.
- `internal/source/` — HTTP fetching, retries, conditional requests.
- `internal/weather/` — `Reading` model and provider selection.
- `internal/metrics/` — OpenTelemetry instruments and recorder.
- `deploy/` — example collector configuration and deployment assets.
- `.context/` — documentation, agent playbooks, workflow state.

## AI Context References
- Documentation index: `.context/docs/README.md`
- Agent playbooks: `.context/agents/README.md`
- Workflow state: `.context/workflow/status.yaml`
