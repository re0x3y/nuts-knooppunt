# Copilot Local Memory — nuts-knooppunt

> This file stores context about the codebase for future code generation and review tasks.
> It supplements the `.github/copilot-instructions.md` with learned facts from analysis and user input.

---

## Architecture Facts

### mCSD Sync Modes
- **Delta Mode**: `_history` + `_since` parameter for incremental sync. Primary mode.
- **Snapshot Mode**: `GET /Resource` for full sync. Used for initial load or 410 Gone fallback. Controlled by `SnapshotModeSupport` config flag.
- Both modes produce a single `fhir.Bundle` of type `transaction` POSTed to the Query Directory.
- Source: `component/mcsd/component.go:381-450`

### Conditional References (_source)
- Resource IDs are stripped before sending to Query Directory (`delete(resource, "id")` in `updater.go:153`).
- All internal references are converted to `_source`-based conditional references: e.g., `Organization/123` → `Organization?_source=https://admin.example.com/Organization/123`.
- This avoids hard ID dependencies and allows the FHIR server to resolve references atomically.
- Source: `component/mcsd/updater.go:150-175`

### Transaction Bundle Ordering
- Bundle entries are NOT topologically sorted (no hierarchy ordering).
- FHIR R4 spec requires compliant servers to process transactions atomically and resolve internal conditional references.
- HAPI FHIR handles this correctly. Non-compliant servers may fail.
- A topological sort (Organizations → Endpoints → HealthcareServices/Locations) would be a defensive improvement.

### Validation Rules
- **URA Authority (Rule 2)**: Organizations must have a URA identifier or a `partOf` chain leading to one. Source: `validator.go:80-129`
- **Orphan Resource Rejection**: HealthcareService needs `providedBy`, Location needs `managingOrganization`, PractitionerRole needs `organization`. Source: `validator.go:192-225`
- **Endpoint Ownership**: Endpoints must be referenced by at least one Organization or valid HealthcareService. Source: `validator.go:230-272`
- **partOf Chain Validation**: Recursive with circular reference detection. Source: `validator.go:143-189`
- **LRZa Name Authority (Rule 1)**: Currently commented out for PoC. Source: `updater.go:115-119`
- **Endpoint `managingOrganization`**: NOT validated. FHIR R4 makes it optional (0..1). Current validation only checks the inverse (org/service → endpoint reference).

### State Persistence
- `StateFile` config: path to JSON file storing `lastUpdateTimes` map (directory key → timestamp).
- Directory key = `fhirBaseURL` or `fhirBaseURL|authoritativeUra` for multi-tenant directories.
- Source: `component/mcsd/component.go:573-623`

---

## Testing Facts

### Build & Test
- `go build .` to build
- `go test -p 1 -v ./...` to run all tests (serial execution with `-p 1`)
- `go test -v ./test/e2e/...` for e2e tests (requires Docker for testcontainers)

### E2E Test Coverage
- Initial sync, incremental updates, endpoint updates, org updates, child org creation
- POST+PUT+PUT deduplication, CREATE+DELETE scenarios
- Test harness: `test/e2e/harness/` with HAPI FHIR testcontainers
- Test vectors: `test/testdata/vectors/` (Sunflower, Care2Cure, LRZa organizations)

### Unit Test Patterns
- Mock FHIR servers via `httptest.NewServer`
- `test.StubFHIRClient` for local query directory
- JSON test fixtures in `component/mcsd/test/`

---

## PoC 10 Readiness

### Happy Flows (1-8): All implemented
- LRZa root discovery, endpoint registration, delta/snapshot sync, incremental updates, DELETE handling.

### Unhappy Flows (1-4): All implemented
- Unauthoritative data rejection, orphan resources, invalid URA, invalid partOf chains — all produce warnings.

### Known Gaps (Non-blocking for PoC 10)
- LRZa Name Authority (Rule 1) commented out
- No ghost update detection (SHA-256 hashing)
- No Prometheus metrics
- No distributed locking (single-instance mutex only)
- Transaction entries not topologically sorted

---

## Output Bundle Observations (from real-world data)

### Format Note
- Some external FHIR servers serialize bundles in Google FHIR proto JSON format (`{"value": "..."}` wrappers), not standard FHIR JSON.
- The Update Client outputs standard FHIR JSON.

### Validation Gaps Identified
- `Endpoint.managingOrganization` is optional in FHIR R4 and NOT validated by the Update Client. Validation only checks the inverse (Organization/HealthcareService references the Endpoint).
- HealthcareService may reference Location resources not present in the same transaction bundle. If those Locations don't exist in the Query Directory, the conditional references will be broken/unresolvable.
- Organizations with URA identifiers are valid root orgs and don't require `partOf` — this is correct behavior.
