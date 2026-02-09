# mCSD Update Client - Implementation Status Analysis

This document provides a comprehensive analysis of the nuts-knooppunt mCSD Update Client implementation against the PoC 10 requirements and IHE mCSD specifications.

## Table of Contents

- [Overview](#overview)
- [Architecture](#architecture)
- [Implementation Status](#implementation-status)
  - [What's Already Implemented](#whats-already-implemented)
  - [What's NOT Yet Implemented](#whats-not-yet-implemented)
- [Responsibility Distribution](#responsibility-distribution)
- [Recommended Implementation Priorities](#recommended-implementation-priorities)
- [Summary](#summary)

---

## Overview

The nuts-knooppunt project implements an **ITI-91 Update Client** that synchronizes mCSD FHIR resources from remote Administration Directories (including LRZa) to a local Query Directory. The implementation follows the Dutch Generic Functions (GF) Addressing architecture.

### Key Transactions

| Transaction | Description | Status |
|-------------|-------------|--------|
| **ITI-91** | Request Care Services Updates (`_history`) | ✅ Implemented |
| **ITI-130** | Request Care Services Updates (Transaction Bundle) | ⚠️ Uses equivalent approach |
| **ITI-90** | Find Matching Care Services (Query) | ❌ Out of scope |

---

## Architecture

```
┌─────────────────────┐         ITI-91 (_history)         ┌──────────────────────┐
│  Remote mCSD        │  ◄──────────────────────────────  │                      │
│  Administration     │                                    │    nuts-knooppunt    │
│  Directories        │  ──────────────────────────────►  │    (Update Client)   │
│  (LRZa, etc.)       │         history bundles            │                      │
└─────────────────────┘                                    └──────────┬───────────┘
                                                                      │
                                                                      │ FHIR Transaction Bundle
                                                                      │ (PUT with conditional updates)
                                                                      ▼
                                                           ┌──────────────────────┐
                                                           │   Local FHIR         │
                                                           │   Query Directory    │
                                                           │   (mcsd.query)       │
                                                           └──────────────────────┘
```

### Data Flow

1. **Query remote directories** using `_history` endpoint (ITI-91) with optional `_since` parameter
2. **Validate resources** against URA authority rules
3. **Build FHIR Transaction Bundle** with conditional PUT/DELETE operations
4. **Send to Query Directory** configured in `mcsd.query.fhirbaseurl`

---

## Implementation Status

### What's Already Implemented

| Feature | Status | Code Location |
|---------|--------|---------------|
| **ITI-91 Update Client (Delta Mode)** | ✅ Done | `component/mcsd/component.go:360-371` - Uses `_history` with `_since` parameter |
| **LRZa as Root Directory** | ✅ Done | Config `mcsd.admin` supports root directories |
| **Directory Discovery** | ✅ Done | `component/mcsd/component.go:273-319` - Discovers mCSD directory endpoints |
| **URA Authority Validation (Rule 1 & 2)** | ✅ Done | `component/mcsd/validator.go` - Validates organizations must have URA or link to parent with URA |
| **Pagination Support** | ✅ Done | `component/mcsd/component.go:497-523` - Uses `fhirclient.Paginate()` |
| **History Deduplication** | ✅ Done | `component/mcsd/component.go:536-565` - Keeps only most recent version |
| **DELETE Handling** | ✅ Done | `component/mcsd/updater.go:37-69` - Processes DELETE operations |
| **Conditional Updates (_source)** | ✅ Done | `component/mcsd/updater.go:106-137` - Uses `meta.source` for idempotency |
| **Reference Conversion** | ✅ Done | `component/mcsd/updater.go:150-178` - Converts to conditional references |
| **OAuth2 Authentication** | ✅ Done | `component/mcsd/component.go:106-119` - Optional OAuth2 for FHIR servers |
| **Parent Organization Tree** | ✅ Done | `component/mcsd/component.go:647-724` - Builds organization hierarchy |
| **Admin Directory Exclusion** | ✅ Done | Config `mcsd.adminexclude` prevents self-loops |
| **Endpoint Unregistration on DELETE** | ✅ Done | `component/mcsd/component.go:226-241` |

### What's NOT Yet Implemented

| Feature | Requirement Source | Notes |
|---------|-------------------|-------|
| **Snapshot Mode (Full Reload)** | poc10_plan.md, care91.md (ADR-14) | Only `_history` is used, no fallback to full search `GET /Resource` for initial load or recovery |
| **410 Gone Fallback** | poc10_plan.md | No automatic fallback to Snapshot Mode when history is unavailable |
| **Ghost Update Detection (Hashing)** | poc10_plan.md | No SHA-256 hash comparison to prevent no-op updates |
| **LRZa Name Authority (Rule 1)** | care91.md | Names from provider directories should be ignored if LRZa already has the name - **not enforced** |
| **OrganizationAffiliation** | mcsd91.md | Resource type not supported (out of scope per spec) |
| **Practitioner** | care91.md | Out of scope per spec, but mentioned in `defaultDirectoryResourceTypes` |
| **Metrics/Observability** | poc10_plan.md | No Prometheus counters for sync operations |
| **Distributed Locking** | poc10_plan.md | No mechanism to prevent concurrent syncs |
| **mTLS for Admin Directories** | care91.md (Security) | Only OAuth2 configured, mTLS not implemented |

---

## Responsibility Distribution

### Keep in nuts-knooppunt (Update Client)

| Responsibility | Reason |
|----------------|--------|
| ITI-91 querying (`_history`) | Core Update Client function |
| LRZa discovery & crawling | Root of trust logic |
| URA authority validation | Security/trust enforcement |
| Directory endpoint discovery | Federation logic |
| Reference transformation | Ensures portable data |
| Delta synchronization state | `_since` tracking |

### Move to Local Service (Query Directory)

| Responsibility | How to Implement | IHE Transaction |
|----------------|------------------|-----------------|
| **Storing resources** | Accept FHIR Transaction Bundles (PUT/DELETE) | Already receiving this |
| **ITI-90 Search/Read** | Implement `GET /Organization?name=...`, `GET /HealthcareService?type=...` etc. | Query Client queries your service |
| **Referential Integrity** | Handle foreign key relationships when storing | DB constraints |
| **Ghost Update Prevention** | Compare incoming resource hash with stored hash before writing | Storage layer |
| **Full Snapshot Support** | Accept bulk loads when nuts-knooppunt implements it | Future |

### Query Directory Requirements

Your local FHIR service configured in `mcsd.query.fhirbaseurl` needs to support:

1. **FHIR Transaction Bundles** - The component sends `Bundle` with `type: "transaction"`
2. **Conditional Updates** - Using `PUT ResourceType?_source=...`
3. **Conditional Deletes** - Using `DELETE ResourceType?_source=...`
4. **`_source` meta parameter** - Resources are tagged with a `meta.source` URL for tracking origin

---

## Recommended Implementation Priorities

### High Priority for nuts-knooppunt

#### 1. Snapshot Mode

Add full search fallback for initial sync:

```go
// When no _since available, use search instead of _history
if !hasLastUpdate {
    entries, _, err = c.query(ctx, client, resourceType, searchParams) // Method already exists
}
```

#### 2. LRZa Name Authority

Ignore `name` from non-LRZa sources when organization has matching URA:

```go
// In validator.go, when org has URA matching LRZa
// Clear the name field if source != LRZa
```

#### 3. 410 Gone Fallback

Detect when history endpoint returns 410 and automatically switch to Snapshot Mode.

### For Your Local Service

#### 1. Support `_source` searches

Required for conditional updates:

```
PUT /Organization?_source=https://lrza.example.com/Organization/123
DELETE /Endpoint?_source=https://provider.example.com/Endpoint/456
```

#### 2. Implement ITI-90 (Optional)

If you want direct queries from Query Clients:

```
GET /Organization?identifier=http://fhir.nl/fhir/NamingSystem/ura|12345
GET /HealthcareService?type=http://snomed.info/sct|394802001
```

---

## Summary

| Component | PoC 10 Requirement | nuts-knooppunt Status | Your Service |
|-----------|-------------------|----------------------|--------------|
| ITI-91 History | ✅ Required | ✅ Implemented | N/A |
| ITI-130 Update | ✅ Required | ⚠️ Uses Transaction Bundle (equivalent) | ✅ Must accept |
| ITI-90 Query | ❌ Out of scope PoC10 | ❌ Not needed | Optional |
| LRZa Integration | ✅ Required | ✅ Implemented | N/A |
| Authority Rules | ✅ Required | ⚠️ Partial (missing name rule) | N/A |
| Snapshot Mode | ✅ Required (ADR-14) | ❌ Not implemented | N/A |
| mTLS | ✅ Required | ❌ Not implemented | N/A |

### Overall Implementation Progress

**~70-75%** of PoC 10 requirements are implemented.

### Main Gaps

1. **Snapshot Mode** - Required for initial load and recovery scenarios
2. **LRZa Name Authority Rule** - Names from provider directories should be ignored
3. **mTLS Support** - Required per GF Addressing security requirements

---

## References

- [IHE mCSD Profile v4.0.0](https://profiles.ihe.net/ITI/mCSD/index.html)
- [NL Generic Functions IG](https://nuts-foundation.github.io/nl-generic-functions-ig/)
- [PoC 10 Plan of Approach](./poc10.md)
- [Implementation Plan](./poc10_plan.md)
