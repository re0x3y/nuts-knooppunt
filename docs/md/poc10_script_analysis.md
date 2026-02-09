# PoC 10 Script Analysis — Update Client Readiness

This document analyzes whether the current `nuts-knooppunt` mCSD Update Client implementation can execute the PoC 10 demo script (happy and unhappy flows), identifies gaps, and addresses the snapshot mode referential integrity question.

---

## Table of Contents

- [Understanding](#understanding)
- [Happy Flow Analysis](#happy-flow-analysis)
- [Unhappy Flow Analysis](#unhappy-flow-analysis)
- [Snapshot Mode & Referential Integrity](#snapshot-mode--referential-integrity)
- [Implementation Plan](#implementation-plan)
- [Summary](#summary)

---

## Understanding

The PoC 10 demo script (`docs/md/script.md`) describes a live demonstration of the mCSD Update Client. The script has two sections:

1. **Happy Flows** — Demonstrating the normal synchronization lifecycle: empty directory → register organizations → add resources → sync → verify → update → re-sync → verify changes.
2. **Unhappy Flows** — Demonstrating validation enforcement: rejecting unauthoritative data, orphan resources, invalid URA identifiers, and invalid partOf chains.

The implementation uses two operating modes:
- **Delta Mode** (`_history` + `_since`): Incremental updates — the primary mode.
- **Snapshot Mode** (`GET /Resource`): Full sync for initial load, recovery, or 410 Gone fallback. Controlled by `SnapshotModeSupport` config flag.

Both modes build a single FHIR `transaction` Bundle and POST it to the Query Directory.

---

## Happy Flow Analysis

### Flow 1: Show empty/not-yet-synced Query Directories

| Step | Script Requirement | Status | Evidence |
|------|-------------------|--------|----------|
| Query the local FHIR Query Directory | Show it's empty | ✅ Ready | The Query Directory is an external FHIR server (e.g., HAPI FHIR). Querying it directly is outside the Update Client's scope — it's a demo action done by the vendor. |

### Flow 2: Register Organization Directory URL at LRZa

| Step | Script Requirement | Status | Evidence |
|------|-------------------|--------|----------|
| Register admin directory URL at LRZa | LRZa has the Organization + Endpoint | ✅ Ready | The Update Client discovers Endpoints from the root directory (LRZa). `component.go:295-354` discovers mCSD directory endpoints from root directory entries and registers them as administration directories. E2E test `Test_mCSDUpdateClient` verifies this flow. |
| Add extra organization to Admin Directory | Per vendor action | ✅ Ready | This is a vendor/admin action on their own FHIR server, not an Update Client responsibility. |

### Flow 3: Add Endpoints, Locations, HealthcareServices

| Step | Script Requirement | Status | Evidence |
|------|-------------------|--------|----------|
| Add resources to Admin Directory | Per vendor action | ✅ Ready | Admin action; resources will be picked up on next sync. |

### Flow 4: Set LRZa as authoritative source

| Step | Script Requirement | Status | Evidence |
|------|-------------------|--------|----------|
| Configure LRZa as root | Config setting | ✅ Ready | `mcsd.admin.<key>.fhirbaseurl` in `config/knooppunt.yml` configures the root directory. The `AdministrationDirectories` map in `Config` supports multiple root directories. |

### Flow 5: Synchronize data from LRZa and organization directories

| Step | Script Requirement | Status | Evidence |
|------|-------------------|--------|----------|
| Kick off synchronization | `POST /mcsd/update` | ✅ Ready | `component.go:192-202` registers the HTTP handler. The `update()` method iterates all administration directories. E2E tests confirm this works end-to-end. |
| Discover organization directory endpoints from LRZa | Automatic during sync | ✅ Ready | `discoverAndRegisterEndpoints()` at `component.go:295-354` handles this. Endpoints with `mcsd-directory-endpoint` payload type are registered as new administration directories. |
| Sync resources from discovered directories | Automatic after discovery | ✅ Ready | After registration, the main `update()` loop at `component.go:275-292` processes newly registered directories in the same sync cycle. |
| Delta mode (`_history` + `_since`) | Incremental sync | ✅ Ready | `component.go:383-418` implements delta mode with `_since` parameter. State is persisted via `StateFile` config. |
| Snapshot mode (full `GET /Resource`) | Initial or 410 fallback | ✅ Ready | `component.go:388-393` uses snapshot mode when no `_since` exists and `SnapshotModeSupport` is enabled. `component.go:400-411` falls back on 410 Gone. |

### Flow 6: Query the searchable address book

| Step | Script Requirement | Status | Evidence |
|------|-------------------|--------|----------|
| Query the Query Directory | Show synced resources | ✅ Ready | Querying the FHIR Query Directory is a direct FHIR operation. E2E tests (`Test_mCSDUpdateClient`) verify organizations, endpoints, and meta.source are correctly synced. |

### Flow 7: Implement a change in the organization address book

| Step | Script Requirement | Status | Evidence |
|------|-------------------|--------|----------|
| Update a resource in Admin Directory | Per vendor action | ✅ Ready | Vendor action on their FHIR server. |

### Flow 8: After sync, change is visible

| Step | Script Requirement | Status | Evidence |
|------|-------------------|--------|----------|
| Incremental sync picks up change | `_since` parameter | ✅ Ready | E2E test `Test_mCSDUpdateClient_IncrementalUpdates` verifies: updated endpoints, updated organizations, new child organizations, and POST+PUT+PUT deduplication scenarios. |
| DELETE handling | Deleted resources removed | ✅ Ready | `updater.go:57-90` handles DELETE operations. E2E test `CREATE+DELETE scenario` confirms end-to-end deletion. `processEndpointDeletes()` also unregisters directories when their Endpoint is deleted. |

**Happy Flow Verdict: ✅ All 8 happy flow steps are supported by the current implementation.**

---

## Unhappy Flow Analysis

### Flow 1: Data not belonging to own organisation

> *"What if there is data in an admin directory that does not belong to the own organisation?"*  
> Desired behavior: do not adopt → warning

| Check | Status | Evidence |
|-------|--------|----------|
| URA scope authority (Rule 2) | ✅ Implemented | `ensureParentOrganizationsMap()` at `component.go:769-818` builds an org tree filtered by `authoritativeUra`. `ValidateUpdate()` in `validator.go:33-61` validates every resource against this map. Any Organization not linked to the authoritative URA is rejected. |
| HealthcareService must reference valid org | ✅ Implemented | `validateHealthcareServiceResource()` at `validator.go:192-198` checks `providedBy` references a known organization. |
| Location must reference valid org | ✅ Implemented | `validateLocationResource()` at `validator.go:219-225` checks `managingOrganization`. |
| Endpoint must be referenced by valid org/service | ✅ Implemented | `validateEndpointResource()` at `validator.go:210-216` and `assertOrganizationOrHealthcareServiceHasEndpointReference()` at `validator.go:230-272`. |
| Warning produced | ✅ Implemented | `buildUpdateTransaction()` returns errors that are collected as warnings in `component.go:499-504`. |

### Flow 2: Resources without reference to an organisation

> *"HealthcareService, Location, PractitionerRole without reference to an organisation"*  
> Desired behavior: do not adopt → warning

| Check | Status | Evidence |
|-------|--------|----------|
| HealthcareService without `providedBy` | ✅ Implemented | `validateHealthcareServiceResource()` returns error if `ProvidedBy` is nil. |
| Location without `managingOrganization` | ✅ Implemented | `validateLocationResource()` returns error if `ManagingOrganization` is nil. |
| PractitionerRole without `organization` | ✅ Implemented | `validatePractitionerRoleResource()` at `validator.go:201-207` returns error if `Organization` is nil. |
| Warning produced | ✅ Implemented | Same pattern — error becomes a warning in the report. |

### Flow 3: Invalid URA identifier

> *"URA-identifier of organisation is invalid"*  
> Desired behavior: do not adopt → warning

| Check | Status | Evidence |
|-------|--------|----------|
| Multiple URA identifiers | ✅ Implemented | `validateOrganizationResource()` at `validator.go:80-90` rejects organizations with multiple URA identifiers. |
| URA must match authoritative parent | ✅ Implemented | `validator.go:104-113` checks that an organization's URA matches one of the parent URA identifiers. |
| Missing URA without partOf | ✅ Implemented | `validator.go:117-121` rejects organizations without URA and without `partOf`. |
| Warning produced | ✅ Implemented | Same pattern. |

### Flow 4: Invalid partOf (parent organisation) reference

> *"partOf(parent organisation) of organisation is invalid"*  
> Desired behavior: do not adopt → warning

| Check | Status | Evidence |
|-------|--------|----------|
| `partOf` chain validation | ✅ Implemented | `validatePartOfReferencesAuthoritativeOrg()` at `validator.go:133-140` + `validatePartOfChain()` at `validator.go:143-189` recursively validate the partOf chain until an org with URA is found. |
| Circular reference detection | ✅ Implemented | `validatePartOfChain()` tracks visited organizations and returns error on circular references at `validator.go:154-158`. |
| Referenced org not found | ✅ Implemented | `validator.go:187-189` returns error if the referenced organization is not in the parent map. |
| Warning produced | ✅ Implemented | Same pattern. |

**Unhappy Flow Verdict: ✅ All 4 unhappy flow scenarios are handled with appropriate validation and warning generation.**

---

## Snapshot Mode & Referential Integrity

### The Question

> *"In snapshot mode, the update client sends out transactional FHIR bundles but they are not in hierarchy. Should the external FHIR receiver implement the referential integrity?"*

### Analysis

#### How the Current Implementation Works

Both Delta and Snapshot modes produce the same output: a single `fhir.Bundle` of type `transaction` (`component.go:492`). The entries are **not ordered** by resource dependency hierarchy. For example, a `HealthcareService` with `providedBy: Organization/123` might appear before `Organization/123` in the bundle.

However, the implementation uses **conditional references** via `_source` parameters (`updater.go:150-175`). All internal `Organization/123` references are converted to `Organization?_source=<sourceURL>` before being sent. This means:

1. **The FHIR server receives conditional PUTs** — e.g., `PUT Organization?_source=https://admin.example.com/Organization/123`
2. **References between resources are also conditional** — e.g., `providedBy.reference = "Organization?_source=https://admin.example.com/Organization/123"`

#### FHIR Transaction Processing Rules (FHIR R4 Spec)

Per the [FHIR R4 specification on transactions](http://hl7.org/fhir/R4/http.html#transaction):

> *"A FHIR server processing a transaction SHALL process the actions in a defined order... DELETE → POST → PUT/PATCH → GET. Within each category, the server processes entries in the order they appear."*

Additionally:

> *"When processing the transaction, the server SHALL resolve any references to other resources within the bundle... If any resource identities (including resolved identities from conditional update/create) overlap, the transaction SHALL fail."*

This means a **compliant FHIR server** MUST:
1. Process all entries in the transaction as an atomic unit
2. Resolve conditional references (`?_source=...`) to actual resource IDs
3. Handle the ordering internally (DELETE before PUT, etc.)

#### Answer: No, the External FHIR Receiver Should NOT Need Special Referential Integrity Logic

**The FHIR specification already requires transaction processing to be atomic and to resolve internal references.** Specifically:

| Concern | FHIR Spec Handling |
|---------|-------------------|
| **Order of operations** | FHIR servers MUST process transaction entries in spec-defined order (DELETE → POST → PUT → GET), regardless of bundle entry order |
| **Conditional references** | The `_source`-based conditional references (`Organization?_source=...`) are resolved by the FHIR server during transaction processing |
| **Atomicity** | If any entry fails, the entire transaction rolls back |
| **Cross-entry references** | FHIR servers resolve references to resources *within the same transaction bundle* |

#### However, There Are Practical Considerations

1. **Not all FHIR servers implement the full spec.** HAPI FHIR (the common choice) handles transaction ordering and conditional references correctly. But simpler or custom FHIR servers might not.

2. **Conditional PUT ordering within the same verb category.** The FHIR spec says entries within PUT are processed *in order*. If `HealthcareService` (with a conditional reference to `Organization`) appears before `Organization` in the bundle, the FHIR server processes `Organization` PUT first only if the server reorders them internally. Most compliant servers (HAPI FHIR included) do handle this correctly because they resolve all conditional references **before** executing operations.

3. **The `_source`-based approach avoids hard ID dependencies.** Since `nuts-knooppunt` strips resource IDs (`delete(resource, "id")` in `updater.go:153`) and uses `_source` for conditional matching, there's no hard foreign-key style referential integrity to enforce. The references are flexible query-based references.

#### Recommendation

| Approach | Pros | Cons |
|----------|------|------|
| **Current approach (no ordering)** | Simple, spec-compliant FHIR servers handle it | May fail on non-compliant servers |
| **Topological sort in transaction bundle** | More resilient across FHIR server implementations | Added complexity in the Update Client; the poc10_plan.md mentions this as "Ideal" |
| **Multi-pass transactions** | Guaranteed ordering (Organizations first, then dependents) | Multiple HTTP calls; loss of atomicity |

**Current approach is correct for spec-compliant FHIR servers.** If broader FHIR server compatibility is needed, implementing a topological sort (Organizations → Endpoints → HealthcareServices/Locations) in the transaction bundle would be a defensive improvement but is not strictly required.

---

## Implementation Plan

Based on the analysis, the current implementation is **ready to execute the PoC 10 demo script**. Below are optional improvements, ordered by priority:

### Already Working (No Changes Needed)

- [x] Happy Flows 1-8: All synchronization, discovery, incremental update, and deletion scenarios
- [x] Unhappy Flows 1-4: All validation rules (URA authority, orphan resources, invalid identifiers, invalid partOf chains)
- [x] Delta Mode with `_since` state persistence
- [x] Snapshot Mode with 410 Gone fallback
- [x] Deduplication of `_history` entries
- [x] Conditional `_source`-based updates (idempotent)
- [x] Reference conversion to conditional references

### Optional Improvements (Not Blocking PoC 10)

| Priority | Feature | Rationale |
|----------|---------|-----------|
| Low | **Topological sort of transaction bundle entries** | Defensive measure for non-compliant FHIR servers. Add Organization entries before dependents. |
| Low | **LRZa Name Authority (Rule 1)** | Currently commented out (`updater.go:115-119`). Stripping names from provider directories when LRZa has the name. Marked as "TODO: commented out for PoC." |
| Low | **Ghost Update Detection (SHA-256 hashing)** | Prevents no-op writes. Not required for correctness, only for efficiency. |
| Low | **Metrics/Observability** | Prometheus counters for sync operations. Nice-to-have for production. |
| Low | **Distributed Locking** | Prevent concurrent syncs. The `updateMux` mutex handles single-instance locking already (`component.go:275`). |

---

## Real-World Output Bundle Analysis

Two real-world output transaction bundles were analyzed (`.test/data1.json` from NLcom, `.test/data2.json` from HINQ).

> **Note:** These files use Google FHIR proto JSON serialization (`{"value": "..."}` wrappers), not standard FHIR JSON. The Update Client outputs standard FHIR JSON.

### data1.json (NLcom) — 4 entries

| Entry | Resource | Observations |
|-------|----------|-------------|
| 1 | Endpoint `87a449d6` (query-directory-reads) | No `managingOrganization`. **Valid** — org `79014519` references it. |
| 2 | Organization `79014519` (NLcom test, URA `733208443`) | No `partOf`. **Valid** — it IS the root org (has URA). References 3 endpoints. |
| 3 | Endpoint `f5ada982` (Nuts OAuth) | No `managingOrganization`. **Valid** — org references it. |
| 4 | Endpoint `fc9b871f` (eOverdracht Server) | No `managingOrganization`. **Valid** — org references it. |

**Verdict: ✅ Valid.** Single root org with URA owns all 3 endpoints. No hierarchy needed.

### data2.json (HINQ) — 6 entries

| Entry | Resource | Observations |
|-------|----------|-------------|
| 1 | Endpoint `cc6ee366` (eOverdracht notification) | No `managingOrganization`. **Valid** — child org `e95a62f4` references it. |
| 2 | HealthcareService `67942ff3` | Has `providedBy` → org `e95a62f4`. References 2 Locations (`b7a604ec`, `5f77b968`) and 2 Endpoints. **⚠️ Locations not in bundle.** |
| 3 | Organization `e95a62f4` (HINQ eOverdracht Organisatie, URA `00000023`) | Has `partOf` → org `9a2ad1f4`. **Valid.** |
| 4 | Organization `9a2ad1f4` (HINQ ZNO, URA `00000023`) | No `partOf`. **Valid** — it IS the root org (has URA). |
| 5 | Endpoint `c24134fe` (query-directory-reads) | No `managingOrganization`. **Valid** — root org `9a2ad1f4` references it. |
| 6 | Endpoint `f4cb655c` (Nuts OAuth) | No `managingOrganization`. **Valid** — child org `e95a62f4` references it. |

**Verdict: ✅ Valid with a caveat.** The HealthcareService references 2 Location resources that are NOT in this transaction bundle. If those Locations don't already exist in the Query Directory, the conditional `_source` references will be dangling (unresolvable).

### Are the bundles "proper"?

**Yes, both bundles pass the current Update Client validation rules.** Here's why the user's concerns are non-issues:

| Concern | Explanation |
|---------|-------------|
| **Endpoints without `managingOrganization`** | `Endpoint.managingOrganization` is **optional** (0..1) in FHIR R4. The Update Client validates the inverse relationship: it checks that each Endpoint is referenced by at least one Organization or HealthcareService via their `endpoint[]` field (`validator.go:230-272`). All endpoints in both bundles pass this check. |
| **Organizations without `partOf`** | Valid when the Organization has a URA identifier — it's the **root/authoritative organization**. The validator (`validator.go:117-121`) only requires `partOf` when an Organization lacks a URA identifier. Both root orgs (`79014519` with URA `733208443`, `9a2ad1f4` with URA `00000023`) correctly have no `partOf`. |

### Should the Update Client add more validation?

The current validation is correct per the mCSD spec. However, these observations suggest areas to monitor:

| Area | Current Behavior | Potential Improvement |
|------|-----------------|----------------------|
| **Missing Location resources in bundle** | Not checked — Locations referenced by HealthcareService may not be in the same transaction | Could warn when a HealthcareService references Locations that aren't in the current batch |
| **`Endpoint.managingOrganization`** | Not required | Could be recommended (warning-level) for better data quality, but NOT required per FHIR R4 |
| **Cross-bundle reference integrity** | Not tracked | Could track which `_source` references exist in the Query Directory and warn about dangling ones |

**The Update Client does NOT need to filter these bundles** — they are valid. The validation already covers the PoC 10 unhappy flows (unauthoritative data, orphan resources, invalid URA, invalid partOf chains). The Endpoint and Organization patterns seen in these bundles are correct per the specification.

---

## Summary

| Area | Status |
|------|--------|
| **Happy Flows (1-8)** | ✅ Fully supported |
| **Unhappy Flows (1-4)** | ✅ Fully supported |
| **Delta Mode** | ✅ Implemented with `_since` + state persistence |
| **Snapshot Mode** | ✅ Implemented with 410 Gone fallback |
| **Referential Integrity in Transaction Bundles** | ✅ Not needed — FHIR spec requires servers to handle transaction atomicity and internal reference resolution. The `_source`-based conditional approach avoids hard ID dependencies. |
| **Real-World Bundle Validity (data1.json, data2.json)** | ✅ Both valid per current validation rules |
| **PoC 10 Demo Readiness** | ✅ Ready |
