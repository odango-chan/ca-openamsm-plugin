# AC-009 module verification — OpenAMSM Event/Delivery Plugin

**Status:** MODULE PASS with a test Host and HTTP/2 test peer. **NOT** Next Python Host acceptance, **NOT** product AC PASS, **NOT** system OM/E2E, **NOT** Production Qualification.
**Tested source tree:** `4a2edd386d4ed9bdcacedd122b47285af13c49e1` on `feature/issue-2-sr-009-openamsm-plugin` (contains implementation, tests, CI, docs). This evidence file is added in a later docs-only commit; it changes no tested code.
**Design pins:** Next `ba5354c575394295cfa7a790922e02422b0c96f4` (PR #115 Head, Base `b2ed5eb84114f9c2556a0cd06ed974887b1b70ad`); plugin design `5bb987aec6a643d453e7d2f111c04c4f02dbcd2f` (PR #1 Head).
**Tracking:** this repository Issue #2; review ca-agent-next Issue #116.

## 1. Environment and commands

| Item | Value |
|---|---|
| Local OS | Windows 11, amd64 |
| Local Go | `go1.26.6 windows/amd64` |
| CI | GitHub Actions `Plugin module CI` run [46123776882](https://github.com/odango-chan/ca-openamsm-plugin/actions/runs/46123776882) — ubuntu-latest + windows-latest, both **SUCCESS** at `4a2edd3` |
| Formatting | `gofmt -l .` → empty |
| Vet | `go vet ./...` → PASS |
| Full suite | `go test ./... -count=1` → PASS (8 packages with tests; 3 packages without test files) |
| Race | `go test ./... -count=1 -race` → PASS (local Windows; CI ubuntu race step PASS) |
| Build | `go build -trimpath ./cmd/openamsm-plugin` → PASS |

**Totals:** 87 top-level test functions, 123 passing test cases (including subtests), 1 skipped (Windows symlink privilege; Linux CI runs it and passes).

Per-AC commands:

```bash
go test ./internal/ipc/... ./internal/plugin/... -run AC00901 -count=1
go test ./internal/sbi/... ./internal/staging/... ./internal/plugin/... -run AC00902 -count=1
go test ./internal/staging/... ./internal/plugin/... -run AC00903 -count=1
go test ./internal/event/... -run AC00904 -count=1
go test ./internal/delivery/... ./internal/sbi/... ./internal/plugin/... -run AC00905 -count=1
go test ./internal/plugin/... -run AC00906 -count=1
go test ./internal/manifest/... -run OAP01 -count=1
```

## 2. Per-AC results

| AC | Command (above) | Top-level tests | Cases | Result |
|---|---|---|---|---|
| AC-009-01 | `-run AC00901` | 11 | 23 | PASS (module) |
| AC-009-02 | `-run AC00902` | 26 | 26 | PASS (module) |
| AC-009-03 | `-run AC00903` | 5 | 5 (+1 skipped locally; PASS on Linux CI) | PASS (module) |
| AC-009-04 Event mapping/Schema/test-double | `-run AC00904` + `-run OAP01` schema tests | 17 (+8 OAP-01) | 36 (+8) | PASS (module, test-double mapping only) |
| AC-009-04 production T0 classifier binding | not run | 0 | 0 | **BLOCKED_EXTERNAL_INPUT** |
| AC-009-05 | `-run AC00905` | 18 | 23 | PASS (module) |
| AC-009-06 | `-run AC00906` | 2 | 2 | PASS (module) |

### AC-009-01 — IPC lifecycle, handshake, version/extension match

Positive: real binary handshake (`host.initialize` → `plugin.ready` with pluginId/version/IPC version and the exact two-extension set), shutdown with `plugin.stopped.inFlightCount` and exit 0; shutdown with a live ingress reports a non-zero in-flight count and terminates the stream.
Negative: wrong envelope `apiVersion`, initialize identity/version mismatch, missing `artifactStaging`, unknown message type, duplicate `messageId`, unsupported argv, oversized frame, duplicate JSON key, non-object payload, unknown envelope field, duplicate/oversized frames, ACK payload injection (`runId`, `skillId`, `artifactRef`, `responseObligationId`, `deliveryId`) → process exits 2 with no success frame. 256 concurrent frame writes parse as 256 intact frames.

### AC-009-02 — Create/Update JSON + multipart Content-ID + raw staging

Positive: Create with N1+N2; Create without N2; Create/Update with `n2SmInfoExt1` → `n2Ext1.request`; Update `0xC9` and continuation N2-only (test-only double).
Byte fidelity: staged inbound bytes equal fixtures exactly; multipart response N1 bytes equal the Host-staged outbound bytes exactly; JSON `request` object preserved byte-for-byte (including unknown/future fields and original order).
Negative: missing referenced part, duplicate Content-ID, unreferenced extra part, wrong media type, duplicate reference of one part by two fields, missing/duplicate JSON part, part without Content-ID, type without part, Create with `application/json` only, unsupported method/path, invalid JSON, malformed multipart, oversized body, empty binary part, invalid event id, outbound slot used as inbound, missing staging roots.
Production classifier: Update ingress returns HTTP 503 `T0_CLASSIFIER_BLOCKED_EXTERNAL_INPUT`, emits no `ingress.event` and leaves no inbound staging residue.

### AC-009-03 — staging containment

Negative: absolute path, drive/volume path, `../` traversal (both separators), alternate-data-stream `:` path, empty path; symlink escape (skipped only where the OS cannot create symlinks); missing file; empty file; declared/actual byteLength mismatch; unknown slot; inbound slot used as outbound; outbound slot/media-type mismatch; incomplete outbound descriptor in a real dispatch → `failed` result + HTTP 502 (no 2xx).
Structural: descriptor JSON exposes exactly `slot`, `mediaType`, `contentId`, `stagingPath`, `byteLength`; no `artifactRef`, no Host identity, no extra digest.

### AC-009-04 — mapping, Schema, test-double; production binding blocked

Positive: three frozen Event types; `operationId` `PostSmContexts`/`UpdateSmContext`; verbatim `request`; discriminators `0xC9`/`0xD1`/`0xD4`; the three N2 completions; dual-path `0xD4` + `PDU_RES_REL_RSP`; N1/N2 separately; optional `n2.request`/`n2Ext1.request`; message type read at offset 3 (bytes 1/2 are PSI/PTI).
Negative: conflicting new+continuation discriminators, `0xD4` + request type, truncated N1, wrong EPD, unsupported message type, missing N1, N2 type/part mismatches, unknown N2 type (outside the frozen 26-value TS 29.502 enumeration), continuation Ext1 → `N2_EXT1_CONTINUATION_UNSPECIFIED` (sub-item NOT_RUN).
Schema: `plugin.yaml` validates against Plugin Package v1alpha1 with closed objects, exact `const` operationIds, required fields and slot/media declarations; emitted payloads satisfy the declared schemas (closed objects, const/enum/anyOf/required).
Boundary: payload keys are only `operationId`, optional `smContextRef`, `request`, optional `discriminator`; no `runId`, `skillId`, `correlationId`, `responseObligationId`, `deliveryId`, `artifactRef` or capability grant.
**Production binding blocked:** `ca-client-tools#44` is OPEN, the latest release is v1.2.0 (2026-11-24), and the narrow `0xC9`/`0xD1`/`0xD4` classifier API is not published. The production adapter returns `ErrBlockedExternalInput` for every Update classification. No NAS parser was written and full `DecodeNAS` is not called.

### AC-009-05 — responseChannel / delivery / obligation identity

Positive: triple binding (`deliveryId` + `responseObligationId` + `responseChannelId` + `eventType`); one result per dispatch; delivered statusCode; multipart response Content-ID + byte fidelity; JSON-only and empty-body responses; Host `Location` preserved; forbidden/hop-by-hop headers replaced or dropped.
Negative: unknown channel, missing obligation, wrong event type (does not consume the channel), second dispatch after delivery, ACK identity leakage (process exit 2), cancel live ingress (`cancelled`, HTTP 503), late cancel (`too_late`), unsupported cancel of initialize, invalid status/body/artifact, multipart without JSON data part.
Identity: `delivery.result` payload keys are only `deliveryId`/`disposition`/optional `statusCode`/safe `error`; the `Waiter` struct has no obligation/Run/Skill/artifactRef/deliveryId field (reflection assertion); concurrency-safe claim succeeds exactly once across 32 attempts.

### AC-009-06 — concurrency isolation

Eight concurrent HTTP/2 streams multiplexed over one h2c connection with distinct `externalRequestId`, JSON content and N1 bytes: every stream receives its own response body and its own N1 response bytes, and every event maps to its own Event/Channel/Delivery identities. Two identical retry requests with the same `X-CA-Request-Id` and identical bytes create two independent Event IDs, channels and responses (Host-owned deduplication is explicitly out of module scope). Race detector PASS.

## 3. P2 evidence map (non-blocking rectification)

| P2 | Evidence in this module |
|---|---|
| P2-01 | `n2Ext1.request` declared for Create/Update; Ext1 staging + type validation tested; continuation Ext1 explicitly `N2_EXT1_CONTINUATION_UNSPECIFIED` and marked NOT_RUN |
| P2-02 | Closed ACK allowlist with five leak negative cases (process fail-closed) |
| P2-03 | `ingress.event` asserted to carry no `correlationId`; AC wording uses Content-ID/slot association |
| P2-04 | No capabilities discovery frame; handshake asserts manifest extension set == `plugin.ready.extensions` |
| P2-05 | Triple-binding negatives (missing/wrong obligation, wrong event type, second dispatch) + reflection assertion that the waiter stores no Host identity |
| P2-06 | Create never requires a discriminator (positive Create tests); rule scoped to Update |
| P2-07 | Docs pin `ba5354c`/`5bb987a`; duplicated paragraphs and numbering corrected; F06 §4.1 treated as frozen |
| P2-08 | acceptance.md now separates §1 module gates from §2 system/phase-only gates |
| P2-09 | Registered in the Next SR-009 READY update (separate repo PR) |
| P2-10 | Registered in the Next F05/F06 remediation (separate repo PR) |
| P2-11 | Registered in the Next governance index/status update (separate repo PR) |

## 4. NOT_RUN / explicit exclusions

| Item | State | Why |
|---|---|---|
| AC-009-04 production classifier binding | BLOCKED_EXTERNAL_INPUT | `ca-client-tools#44` unpublished |
| Continuation Ext1 | NOT_RUN | no frozen slot contract; fail closed |
| Next Python Host accepting this manifest | NOT_RUN | merged SR-003 Plugin Manager slice rejects `artifacts` and does not pass `artifactStaging`; SR-010 Host scope |
| Real OpenAMSM Create → Modify → UE Release | NOT_RUN | SR-011 system E2E |
| Owner Receipt/Commit Proof/Reconcile, UNKNOWN | NOT_RUN | Host/Runtime scope |
| Production Qualification | NOT_RUN | out of scope |

## 5. Non-claims

Test Host PASS ≠ Next Python Host PASS. Module PASS ≠ product AC PASS. Module PASS ≠ OpenAMSM/AMF/UPF E2E PASS. The test-only classifier double is not a production classifier PASS. No Base64, no extra SHA-256, no Plugin-owned ArtifactStore or `artifactRef`, no second Codec or IPC protocol was introduced.

## 6. Evidence pin preservation after stacked design update

This is a fixed historical record for source `4a2edd386d4ed9bdcacedd122b47285af13c49e1` and its original Next `ba5354c` / PR #1 `5bb987a` design pins. A later docs-only stacked reconciliation incorporates the revised PR #1 design `8fc1cd2d2530f2fe792f6fe330d5a23c2fe98106`; current Next comparison snapshot is `f8de225054895f53b30f73aa721bd1ca9290813e`. Do **not** recast these older module tests as certifying the newer Next contract or Host-owned obligation identity, and do not treat module PASS as real E2E, Production Qualification or ca-client-tools#44 publication.
