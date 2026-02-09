# Copilot Instructions for nuts-knooppunt

## Project Overview

Nuts Knooppunt is a Go application implementing the [Nuts Knooppunt specifications](https://wiki.nuts.nl/) for Dutch healthcare data exchange. It acts as a "hybrid monolith" — a single binary containing multiple subsystems (addressing/mCSD, localization/NVI, consent/MITZ, authentication/Dezi) that communicate via HTTP even within the same process, enabling future extraction to microservices.

## Build, Test, and Run

```shell
# Build
go build .

# Run all tests (CI uses -p 1 for serial execution)
go test -p 1 -v ./...

# Run a single test
go test -v -run TestMyFunction ./component/mcsd/...

# Run e2e tests only (requires Docker — uses testcontainers)
go test -v ./test/e2e/...

# Development stack (docker compose)
docker compose up --build
docker compose --profile demoehr up --build
```

There is no separate linter configured in CI.

## Architecture

### Component Lifecycle

Every subsystem implements the `component.Lifecycle` interface:

```go
type Lifecycle interface {
    Start() error
    Stop(ctx context.Context) error
    RegisterHttpHandlers(publicMux *http.ServeMux, internalMux *http.ServeMux)
}
```

Components are instantiated with a `New(config)` constructor, then the bootstrap sequence in `cmd/start.go` calls `RegisterHttpHandlers()` on all components, then `Start()`, and on shutdown `Stop()` in order. The tracing component is special — started first and stopped last to capture all telemetry.

### Two HTTP Interfaces

- **Public** (`:8080`) — external-facing endpoints (e.g., `/mcsdadmin`, `/auth`)
- **Internal** (`:8081`) — internal/health endpoints (e.g., `/status`, `/mcsd/update`, `/nvi/*`, `/mitz/*`)

Components register handlers on one or both muxes. Inter-component communication uses HTTP calls to localhost.

### Components

| Component | Path prefix | Purpose |
|-----------|------------|---------|
| `mcsd` | `/mcsd` | mCSD directory synchronization (addressing) |
| `mcsdadmin` | `/mcsdadmin` | Web UI for managing mCSD Administration Directory |
| `status` | `/status` | Health check endpoint |
| `tracing` | — | OpenTelemetry tracing, logging |
| `http` | — | HTTP server lifecycle (public + internal) |

Additional subsystems for NVI (localization), MITZ (consent), and authentication/OIDC are integrated via the Nuts node or additional components.

### Configuration

Configuration is loaded via [koanf](https://github.com/knadh/koanf) in this order (later overrides earlier):

1. Defaults from `DefaultConfig()` functions
2. YAML file: `config/knooppunt.yml`
3. Environment variables with `KNPT_` prefix (e.g., `KNPT_MCSD_QUERY_FHIRBASEURL`)

Each component defines its own `Config` struct with `koanf` tags. The top-level `cmd.Config` composes them all. See `docs/CONFIGURATION.md` for all options.

## Conventions

### Logging

Use `log/slog` (not logrus for new code). Use structured attributes from `lib/logging`:
- `logging.Error(err)` — for errors
- `logging.FHIRServer(url)` — for FHIR server URLs
- `logging.Component(v)` — for component type identification

### FHIR

- FHIR models come from `github.com/zorgbijjou/golang-fhir-models`
- FHIR client operations use `github.com/SanteonNL/go-fhir-client`
- FHIR utilities live in `lib/fhirutil`

### Testing

- Unit tests use `github.com/stretchr/testify` (require/assert)
- Mocks use `go.uber.org/mock` (uber-go/mock)
- E2E tests live in `test/e2e/` and use testcontainers-go to spin up HAPI FHIR servers in Docker
- Test data/fixtures are in `test/testdata/` (a separate Go module with `replace` directive)
- The `test/testdata/vectors/` package provides pre-built FHIR resources for test scenarios (care organizations like "Sunflower" and "Care2Cure")
- Helper utilities: `test.ReadJSON()`, `test.ParseJSON()`, `test.WaitForHTTPStatus()`, `test.TempDir()`
- Component test configs: use `http.TestConfig()` for HTTP config in tests

### Interface Compliance

Components assert interface compliance at package level:
```go
var _ component.Lifecycle = (*Component)(nil)
```

### Multi-tenancy

The `X-Tenant-ID` header identifies the requesting care organization (URA) for NVI and other tenant-aware endpoints.
