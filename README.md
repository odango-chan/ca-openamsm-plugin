# CA OpenAMSM Plugin

Independent Go Event/Delivery Plugin for OpenAMSM SBI protocol ingress and response delivery.

**Normative Host contract:** ca-agent-next Plugin Package / Plugin IPC v1alpha1 (already merged) and the F05 local staging contract.
**Current central design comparison snapshot (not the original tested source contract):** `ca-agent-next` PR #115 `integration/issue-50-mainline-convergence`, fixed revision `f8de225054895f53b30f73aa721bd1ca9290813e` (Base `b2ed5eb84114f9c2556a0cd06ed974887b1b70ad`). PR #115 is Draft and not merged to `main`.
**Merged-into-this-stack design revision:** this repository PR #1, Head `8fc1cd2d2530f2fe792f6fe330d5a23c2fe98106`.
**Independent review:** ca-agent-next [Issue #116](https://github.com/odango-chan/ca-agent-next/issues/116#issuecomment-7235978519) — `APPROVE_FOR_READY（限定范围）`, 0 P0 / 0 P1 / 11 P2.
**Implementation tracking:** this repository [Issue #2](https://github.com/odango-chan/ca-openamsm-plugin/issues/2), branch `feature/issue-2-sr-009-openamsm-plugin`.

## Status (2026-11-28)

| Item | State |
|---|---|
| Go module implementation | IMPLEMENTED on the implementation branch |
| AC-009-01/02/03/05/06 module tests | PASS — see [AC evidence](docs/evidence/AC-009-verification.md) |
| AC-009-04 Event mapping / Schema / test-double | PASS (test-double mapping only) |
| AC-009-04 production T0 classifier API binding | **BLOCKED_EXTERNAL_INPUT** — `ca-client-tools#44` has not published the narrow 5GSM classifier API |
| Next Python Host / ArtifactStore / Channel | NOT_IMPLEMENTED in Next (SR-010) — module tests use a test Host only |
| Real Create → Modify → UE-requested Release E2E | **NOT_RUN** — SR-011 |

Test Host success is **not** Next Python Host acceptance. Module success is **not** real OpenAMSM E2E or Production Qualification. No product AC, system OM/EX or Production Qualification is claimed.

## What this Plugin does

The Plugin handles external SBI HTTP/2 ingress and response delivery:

- JSON (`application/json`) and `multipart/related` parsing;
- Content-ID association for N1/N2/N2 Ext1 binary parts;
- raw N1/N2 staging under the Host-supplied inbound directory;
- normalized Event publication for the three frozen F06 §4.1 types;
- minimal T0 5GSM discriminator on the shared `UpdateSmContext` path;
- one in-memory `responseChannelId` waiter per live HTTP/2 stream;
- SBI response serialization, including multipart/Content-ID and Host-staged outbound N1/N2 responses;
- concurrency isolation, fail-closed errors and shutdown cleanup.

It does **not** own ArtifactStore or `artifactRef`, Run identity, deduplication, continuation, `responseObligationId` lifecycle, Skill selection, Owner calls, Tool dispatch, Codec implementation or a second IPC protocol.

For shared `UpdateSmContext`, the Plugin consumes only the narrow T0 5GSM discriminator API owned by `ca-client-tools` (Modify `0xC9`, UE-requested Release `0xD1`, Release completion `0xD4`). That API is not published yet, so the production adapter fails closed with `BLOCKED_EXTERNAL_INPUT`; it does not copy a NAS parser or call full `DecodeNAS`. Tests may enable a clearly marked test-only double to verify Event mapping.

CA-SL 1.6.0 is the only target; `when` is declared only in `assurance/contract.yaml`. Host Resolver owns Skill selection and continuation. T2 full Decode/Encode is an independent Agent-callable Codec Tool invoked by a selected Skill through the unified Runtime Action Boundary after Skill/Run binding; this Plugin never executes T2.

## Build and test

Requirements: Go 1.26 (see `go.mod`).

```bash
go build -trimpath -o bin/ca-openamsm-plugin ./cmd/openamsm-plugin
go vet ./...
go test ./... -count=1
go test ./... -count=1 -race        # concurrency isolation evidence
```

Per-AC commands:

```bash
go test ./internal/ipc/... ./internal/plugin/... -run AC00901 -count=1
go test ./internal/sbi/... ./internal/staging/... ./internal/plugin/... -run AC00902 -count=1
go test ./internal/staging/... -run AC00903 -count=1
go test ./internal/event/... -run AC00904 -count=1
go test ./internal/delivery/... ./internal/sbi/... ./internal/plugin/... -run AC00905 -count=1
go test ./internal/plugin/... -run AC00906 -count=1
go test ./internal/manifest/... -run OAP01 -count=1
```

The module tests use a real test Host (spawns the built binary and speaks IPC v1alpha1) plus an HTTP/2 test peer (h2c, prior knowledge). Test counts, commands and evidence are recorded in [docs/evidence/AC-009-verification.md](docs/evidence/AC-009-verification.md).

## Deployment configuration

`host.initialize.payload.config` is a closed object:

```json
{
  "peerId": "configured-sbi-peer",
  "externalRequestHeader": "X-CA-Request-Id",
  "sbi": {
    "listenAddress": "0.0.0.0:9042",
    "maxRequestBytes": 16777216,
    "tlsCertFile": "",
    "tlsKeyFile": ""
  },
  "timeouts": { "ackTimeoutMs": 5000, "deliveryTimeoutMs": 30000, "shutdownGraceMs": 5000 },
  "limits": { "maxResponseBytes": 33554432 }
}
```

- `peerId` is a deployment-recognised trusted peer identity; it is never derived from an HTTP header.
- `externalRequestId` is populated only from the configured private header when the trusted peer supplies it; it is not a 3GPP field and is never synthesized.
- `tlsCertFile`/`tlsKeyFile` enable HTTP/2 over TLS. Without them the listener is plaintext h2c (prior knowledge), with no HTTP/1.1 fallback.
- `host.initialize.payload.artifactStaging.inboundDirectory` / `outboundDirectory` are created and owned by the Host. The Plugin writes inbound files only and reads the exact outbound files named by the current dispatch.

`--test-only-classifier-double` exists only so module tests can exercise Update mapping. It is not the manifest argv and must never be used in a production report; production `UpdateSmContext` classification remains `BLOCKED_EXTERNAL_INPUT` until `ca-client-tools#44` publishes and pins the exact API.

## Artifact slots

| Slot | Direction | Media type |
|---|---|---|
| `n1.request` | Inbound | `application/vnd.3gpp.5gnas` |
| `n2.request` | Inbound | `application/vnd.3gpp.ngap` |
| `n2Ext1.request` | Inbound | `application/vnd.3gpp.ngap` |
| `n1.confirmation` | Inbound | `application/vnd.3gpp.5gnas` |
| `n2.confirmation` | Inbound | `application/vnd.3gpp.ngap` |
| `n1.response` | Outbound | `application/vnd.3gpp.5gnas` |
| `n2.response` | Outbound | `application/vnd.3gpp.ngap` |

`n2Ext1.request` completes the F06 §4.1 `n2SmInfoExt1.contentId` mapping (P2-01). Create and Update declare it optional; Update also declares the optional `n2.request`. A continuation Ext1 slot has no frozen contract and is **NOT_RUN** — the ingress fails closed with `N2_EXT1_CONTINUATION_UNSPECIFIED` rather than guessing a slot or silently dropping bytes.

Documents: [architecture](docs/architecture.md) · [implementation notes](docs/implementation.md) · [acceptance](docs/acceptance.md) · [module AC evidence](docs/evidence/AC-009-verification.md)
**Snapshot distinction (2026-11-28):** the tested module code and its original design/source pins remain recorded unchanged in [AC-009 evidence](docs/evidence/AC-009-verification.md). The updated PR #1 design adds the Next F05 v0.4.13 / F06 v0.3.9 / IPC v0.2.8 boundaries; its inclusion as a merge parent does not retroactively qualify code for Next Host, production classifier or real 5GC E2E.
