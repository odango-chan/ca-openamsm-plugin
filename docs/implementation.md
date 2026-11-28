# Implementation notes — SR-009 module

**Branch:** `feature/issue-2-sr-009-openamsm-plugin`
**Design pins:** Next `ba5354c575394295cfa7a790922e02422b0c96f4`; plugin design `5bb987aec6a643d453e7d2f111c04c4f02dbcd2f`.
**Tracking:** this repository Issue #2.

This document records *how* the frozen contract is implemented, including the ingress-profile decisions the review asked to make explicit. It does not add a new architectural requirement and it does not claim system acceptance.

## 1. Module map

| Area | Package / file |
|---|---|
| IPC framing, strict envelope, payload allowlists, NDJSON reader/writer | `internal/ipc` |
| Slot vocabulary and media types | `internal/slot` |
| Local staging containment, byteLength and symlink checks | `internal/staging` |
| Event normalization, T0 classifier seam, frozen N2 enumeration | `internal/event` |
| SBI target parsing, JSON/multipart parsing, Content-ID resolution, response serialization, HTTP/2 listener | `internal/sbi` |
| Response-channel waiters, closed dispatch/ack/result payloads | `internal/delivery` |
| Lifecycle, ingress orchestration, dispatch/cancel/shutdown | `internal/plugin` |
| Manifest conformance loader (OAP-01) | `internal/manifest` |
| Binary entry point | `cmd/openamsm-plugin` |
| Manifest + closed JSON Schemas | `plugin.yaml`, `schemas/` |

## 2. Request → Event mapping

Create (`POST /sm-contexts`, `PostSmContexts`):

| JSON reference | Slot | Required |
|---|---|---|
| `n1SmMsg.contentId` | `n1.request` | yes |
| `n2SmInfo.contentId` | `n2.request` | no |
| `n2SmInfoExt1.contentId` | `n2Ext1.request` | no |

Payload: `{"operationId":"PostSmContexts","request":<verbatim SmContextCreateData>}` with no discriminator. The original JSON is kept as raw bytes and is never rewritten, reordered by the Plugin or defaulted.

Update (`POST /sm-contexts/{smContextRef}/modify`, `UpdateSmContext`):

| N1 classification | N2 classification | Event type | Input slots |
|---|---|---|---|
| `0xC9` | absent or non-completion | `nsmf.update-sm-context` | `n1.request`, optional `n2.request`, `n2Ext1.request` |
| `0xD1` | absent or non-completion | `nsmf.update-sm-context` | same |
| `0xD4` | absent or completion response | `nsmf.update-sm-context.continuation` | `n1.confirmation`, optional `n2.confirmation` |
| absent | completion response | `nsmf.update-sm-context.continuation` | `n2.confirmation` |
| `0xC9`/`0xD1` | completion response | rejected | `CONFLICTING_DISCRIMINATOR` |
| `0xD4` | request/other type | rejected | `CONFLICTING_DISCRIMINATOR` |
| absent | non-completion type | rejected | `MISSING_REQUIRED_SLOT` (`n1.request`) |
| `0xD4` | any Ext1 | rejected | `N2_EXT1_CONTINUATION_UNSPECIFIED` |

Discriminator payload projection:

- Update: `{"n1MessageType":"0xC9"|"0xD1"}` only. `n2SmInfoType` stays in the verbatim `request` object and is not duplicated into the discriminator.
- Continuation: `n1MessageType:"0xD4"` and/or `n2SmInfoType` from the three frozen completions.

## 3. Ingress profile decisions (explicit, fail-closed)

The review required the Ext1 / optional-N2 handling contract to be explicit rather than guessed. These decisions are implemented and tested:

1. `n2Ext1.request` is accepted on Create and Update when `n2SmInfoExt1.contentId` references a part. No continuation Ext1 slot exists in the frozen contract, so a continuation carrying Ext1 is rejected (`N2_EXT1_CONTINUATION_UNSPECIFIED`, P2-01 sub-item NOT_RUN).
2. `n2SmInfoTypeExt1` is optional when an Ext1 part exists (TS 29.502 declares it independently optional). When present it must belong to the frozen `N2SmInfoType` enumeration.
3. An Update N2 part must carry `n2SmInfoType`; otherwise the completion/new-operation classification cannot be decided (`N2_TYPE_MISSING`). Create does not need the type for classification, but the type must still be known if present.
4. A type without a part, or a part referenced by more than one JSON field, is rejected.
5. The accepted `N2SmInfoType` set is the 26-value enumeration from the fixed source `env-openamsm@dc83ccb` / TS 29.502 V19.5.0. Forward-compatible extension strings are outside this ingress profile and fail closed (F06 §4.1 rule 3). This is source data, not an inferred mapping.
6. N1 classification is the F06 minimum header: at least 4 bytes, byte 0 `0x2e`, message type at byte 3. Bytes 1/2 (PSI/PTI) are never treated as the message type.
7. Every multipart binary part must be referenced exactly once by the JSON object; extra parts are rejected rather than ignored. Duplicate Content-IDs, unresolved references and slot/media-type mismatches are rejected.
8. A `multipart/related` request must contain exactly one `application/json` data part. Create is multipart-only per the fixed OpenAPI. Update also accepts `application/json` (no binary), which then fails normalization because a new Update requires N1; this is tested.

## 4. IPC semantics

- **Lifecycle:** `host.initialize` → `plugin.ready` → frames; `host.shutdown` → `plugin.stopped`. Any other first frame, a second initialize, wrong identity/version, missing `artifactStaging`, unknown type, duplicate `messageId`, malformed frame or oversized frame exits with code 2.
- **ingress.event:** contains `eventId`, `ingressId`, `eventType`, optional `externalRequestId`, `responseChannelId`, `source{protocol,peerId}`, normalized `payload` and `artifacts[]`. It never contains `correlationId`, `runId`, `skillId`, `artifactRef` or a capability grant.
- **ingress.ack:** strict allowlist `eventId`, `disposition` ∈ {accepted, duplicate_attached, conflict, rejected}, optional `error{code,message,retryable,details?}`. Injecting any Host identity (`runId`, `skillId`, `artifactRef`, `responseObligationId`, `deliveryId`) terminates the process (P2-02).
- **delivery.dispatch:** closed payload; every dispatch must bind `deliveryId`, `responseObligationId`, `responseChannelId` and `eventType`. Missing/blank identity, invalid status, invalid JSON body or incomplete artifact descriptors produce a `failed` `delivery.result` instead of a false success.
- **delivery.result:** only `deliveryId`, `disposition`, optional `statusCode` and safe `error`. The obligation and channel are never echoed (P2-05).
- **host.cancel:** exactly one of `targetMessageId`/`targetOperationId`; cancels a live ingress, replies `cancelled`, `too_late` or `unsupported`, and the live HTTP stream receives 503 `INGRESS_CANCELLED`.
- **host.shutdown:** stops accepting, cancels live waiters, waits within the grace period and reports the number of live operations at stop.

The production classifier is `event.BlockedClassifier`. It always returns `BLOCKED_EXTERNAL_INPUT` and never parses NAS. The test-only `--test-only-classifier-double` path implements only the four-byte header contract and is recorded as test-double mapping, not production PASS.

## 5. Staging

- The Host creates both roots; `staging.New` fails if either is missing or not a directory.
- Inbound files are written as `event-<eventId>/<slot file>` with `O_EXCL`; file names come from the slot vocabulary, never from peer input. A failed emit removes only that Event's directory; after a successful emit the Host imports and cleans.
- Outbound reads resolve the relative path under the outbound root, reject absolute paths, volume names, `..` traversal, Windows alternate data streams and symlinks, require a regular file, reject empty files and require the real length to equal `byteLength`.
- No Base64 and no additional digest/SHA-256 anywhere in the plugin.

## 6. Response serialization

- Status, headers, body and outbound artifacts are Host-supplied. The Plugin drops hop-by-hop and framing headers (`Content-Type`, `Content-Length`, `Transfer-Encoding`, `Connection`, `TE`, …), then sets its own.
- With outbound parts, the response is `multipart/related` with the JSON data part first, followed by parts carrying `Content-Type` and `Content-ID: <id>`; bytes are copied unchanged.
- Without outbound parts, a non-null JSON body is written as `application/json`; JSON `null` means no body.
- An artifact slot/media-type mismatch, traversal, missing file, empty file or byteLength mismatch rejects the delivery with a safe diagnostic and a 502 to the peer; it never writes a 2xx.

## 7. Concurrency and lifecycle

- Each HTTP/2 stream has an independent waiter holding only Plugin-local `channelId`, `eventId` and `eventType` channels. Host-owned obligation and Run identities are not fields of the waiter (asserted by reflection in tests).
- One dispatch atomically claims the channel; a second dispatch, a wrong event type, an unknown channel or a late dispatch yields a `failed` result and never a second HTTP response.
- Frame writes are mutex-serialized; frame reads are single-threaded.
- Shutdown cancels waiters, waits for handlers within the grace period and then reports the in-flight count.

## 8. Known gaps and NOT_RUN items

| Item | State | Reason |
|---|---|---|
| AC-009-04 production T0 classifier binding | BLOCKED_EXTERNAL_INPUT | `ca-client-tools#44` has not published the exact Go package/API/version |
| Continuation Ext1 slot | NOT_RUN | No frozen continuation Ext1 slot contract; fails closed |
| Next Python Host accepting this manifest | NOT_RUN | The merged SR-003 Plugin Manager slice rejects `artifacts` declarations and does not pass `artifactStaging`; that Host-side gap belongs to SR-010. Module acceptance uses a test Host that implements F05 staging |
| Real OpenAMSM Create → Modify → UE Release | NOT_RUN | SR-011 system E2E |
| TLS-mode listener test | NOT_RUN | Implemented (HTTP/2 over TLS) but module evidence exercises the h2c path; TLS runs only with deployment certificates |
| Symlink-escape test on Windows without symlink privilege | SKIP | The test skips when the host cannot create a symlink; Linux CI covers it |

No result in this repository may be reported as Next Python Host PASS, product AC PASS, OM/EX PASS or Production Qualification.

## 9. Later design-base reconciliation (2026-11-28)

The **original implementation/tested source** remains `4a2edd386d4ed9bdcacedd122b47285af13c49e1`, based on the earlier Next design `ba5354c` and Plugin design `5bb987a`; do not rewrite historical pins as if the test run exercised a later contract. The updated source design PR #1 at `8fc1cd2d2530f2fe792f6fe330d5a23c2fe98106` was merged into the existing PR #3 branch without touching Go, Schema or test files. The current central comparison design is Next PR #115 at `f8de225054895f53b30f73aa721bd1ca9290813e` (F05 v0.4.13/F06 v0.3.9/IPC v0.2.8/SR-009 v0.2.15). Only the original module CI was PASS; changed/new contract subcases still require separate tests or remain NOT_RUN, and the Host three-way obligation identity is not visible to this Plugin.
