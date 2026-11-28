# OpenAMSM Go Plugin — Deployment Adapter Design

**Status:** module implementation IMPLEMENTED on `feature/issue-2-sr-009-openamsm-plugin`. Module AC evidence is recorded in [AC-009 evidence](evidence/AC-009-verification.md); production T0 classifier binding is `BLOCKED_EXTERNAL_INPUT`; Next Host integration and real E2E remain NOT_RUN.

**Current central design candidate:** `ca-agent-next` `integration/issue-50-mainline-convergence` (PR #115, Draft, not merged to `main`), fixed Head `ba5354c575394295cfa7a790922e02422b0c96f4`, Base `b2ed5eb84114f9c2556a0cd06ed974887b1b70ad`.

- [Plugin Package / IPC v1alpha1](https://github.com/odango-chan/ca-agent-next/blob/ba5354c575394295cfa7a790922e02422b0c96f4/docs/%E8%A7%84%E8%8C%83/%E6%8F%92%E4%BB%B6%E5%8C%85%E4%B8%8E%E8%BF%9B%E7%A8%8B%E9%80%9A%E4%BF%A1%E5%8D%8F%E8%AE%AE%E8%A7%84%E8%8C%83.md)
- [F05 Context / Artifact staging](https://github.com/odango-chan/ca-agent-next/blob/ba5354c575394295cfa7a790922e02422b0c96f4/docs/%E8%AE%BE%E8%AE%A1%E8%AF%B4%E6%98%8E%E4%B9%A6/F05-Context%E4%B8%8EArtifact%E7%AE%A1%E7%90%86/Context%E4%B8%8EArtifact%E7%AE%A1%E7%90%86%E8%AE%BE%E8%AE%A1%E8%AF%B4%E6%98%8E%E4%B9%A6.md)
- [F06 OpenAMSM Event contract](https://github.com/odango-chan/ca-agent-next/blob/ba5354c575394295cfa7a790922e02422b0c96f4/docs/%E8%AE%BE%E8%AE%A1%E8%AF%B4%E6%98%8E%E4%B9%A6/F06-%E4%B8%9A%E5%8A%A1%E6%8E%A5%E5%85%A5%E4%B8%8E%E9%9B%86%E6%88%90/%E4%B8%9A%E5%8A%A1%E6%8E%A5%E5%85%A5%E4%B8%8EOpenAMSM%E9%9B%86%E6%88%90%E8%AE%BE%E8%AE%A1%E8%AF%B4%E6%98%8E%E4%B9%A6.md)
- [SR-009 module requirements](https://github.com/odango-chan/ca-agent-next/blob/ba5354c575394295cfa7a790922e02422b0c96f4/docs/SR/SR-009-OpenAMSM%20SBI%20%E4%BA%8B%E4%BB%B6%E6%8F%92%E4%BB%B6%E4%B8%8E%E4%BA%8C%E8%BF%9B%E5%88%B6%E4%BA%A7%E7%89%A9%E4%BA%A4%E6%8E%A5.md)

The Plugin Package / IPC v1alpha1 contract is already merged in Next. The current F05/F06/SR-009 snapshot is the pinned PR #115 Head above. F06 §4.1 is the **frozen** Event/operation/discriminator/slot authority: the implementation generates its schemas and fixtures from it and does not re-open the mapping as a "pre-implementation check".

This repository does not define another Plugin API, IPC version, Skill selector, Artifact service or Tool contract.

## 1. Purpose and strict boundary

ca-openamsm-plugin is an independent Go process that adapts OpenAMSM external SBI protocol ingress and response delivery to the generic CA Agent Host Plugin contract.

It owns:

- external Nsmf/Namf HTTP/2 SBI listener and wire protocol handling;
- multipart parsing and Content-ID association;
- protocol-to-normalized-Event mapping;
- raw N1/N2 binary extraction and Host Artifact Handoff; minimal NAS discriminator extraction only when required for Event normalization; original bytes preserved without eager deep decoding;
- SBI response serialization, including Host-supplied status, headers and public resource URI.

It does not own:

- Agent, LLM Model Loop, Skill selection or execution mode;
- Skill package loading, CA-SL Compiler or SkillCatalog;
- RunStore, Run lifecycle, continuation, retry/replay or response obligations;
- resource/alias mapping or public resource URI generation;
- Runtime, Capability authorization, Tool dispatch or Owner outcome interpretation;
- OpenAMSM Owner calls, MAS Callable mapping or ToolCatalog;
- Artifact database, Artifact namespace or durable business state.

ca-client-tools remains the OpenAMSM Tool implementation. The Event Plugin must never become a second Provider or direct Owner client.

## 2. One normative Plugin Package and IPC

The Plugin implements the exact native contract defined by ca-agent-next Plugin Package v1alpha1:

- manifest `apiVersion: connectionagent.io/plugin-package/v1alpha1`;
- process protocol `connectionagent.io/plugin-ipc/v1alpha1`;
- YAML `plugin.yaml` and JSON Schemas;
- extensions `event.ingress` and `event.delivery` only;
- one long-lived stdio NDJSON process;
- the common message envelope, correlation rules, error shape and lifecycle.

No `ca-agent-host.plugin/v0`, alias, compatibility adapter or Plugin-local message protocol is implemented. The old ca-agent-demo Plugin Contract v0 and PR #249 are historical design inputs only.

The Plugin is an ordinary standalone Go executable, not Go's in-process `plugin/.so` mechanism. Host starts it with operator-approved argv without shell interpretation. stdout carries IPC frames only; diagnostics go to stderr. Concurrent frame writes are serialized.

### 2.1 Manifest

`plugin.yaml` declares:

- `metadata.id: urn:connectionagent:plugin:openamsm`, `version: 0.1.0`;
- `process.executable: bin/ca-openamsm-plugin`, `args: [serve]`;
- three `event.ingress.events` with `requestSchema` and `artifacts.input[]`;
- three `event.delivery.responses` with `responseSchema` and, for Create/Update, `artifacts.output[]`.

The Event names are normalized internal types. Their mapping to OpenAPI operationIds and request fields is frozen by F06 §4.1; this repository does not invent another mapping. The manifest declares no Skill ID, Goal, business Contract Package, execution mode or authorization grant.

### 2.2 IPC lifecycle

`host.initialize` carries plugin identity expectations, `config` and `artifactStaging`. The Plugin answers `plugin.ready` with `pluginId`, `pluginVersion`, `ipcVersion` and the exact extension set. Any mismatch, unknown message, duplicate `messageId`, wrong `apiVersion`, invalid payload or oversized frame fails closed with exit code 2.

`ingress.ack` accepts only `eventId`, `disposition` and an optional safe `error`; any `runId`, `skillId`, `artifactRef`, `responseObligationId` or `deliveryId` is a protocol violation and terminates the process.

`delivery.dispatch` is validated as a closed payload. `delivery.result` returns only `deliveryId`, disposition, optional `statusCode` and safe diagnostics; it never echoes Host-owned obligation/channel identity or a Run ID. `host.cancel` targets the Plugin-local ingress message or Event; `host.shutdown` stops the listener, cancels live waiters and reports `plugin.stopped.inFlightCount`.

## 3. Skill-first event consumption

Every fresh business Event must bind a Skill before execution. There is no unbound Generic LLM Agent fallback.

The Plugin publishes only:

- declared normalized `eventType`;
- the validated normalized payload frozen by F06 §4.1;
- protocol source and optional `externalRequestId` only when the trusted peer supplies a stable request identity;
- raw Artifact staging descriptors. Host `artifactRef` values remain Host-internal and are never sent to this Plugin.

It does not select or name a Skill. It does not decode N1/N2 for business reasoning. For shared `UpdateSmContext` it consumes only the narrow T0 5GSM discriminator API published by `ca-client-tools`: `0xC9`, `0xD1`, `0xD4` or explicit malformed/unsupported. It does not implement or copy a NAS parser, invoke full `DecodeNAS` as a shortcut, or expose business IE fields before Skill selection. Until that API and version are published and pinned, production UpdateSMContext classification fails closed (`BLOCKED_EXTERNAL_INPUT`); a test double may verify Event mapping only. The Host Resolver selects the Skill. Deeper protocol fields remain available through on-demand Codec Tools.

### 3.1 On-demand protocol decoding (T2)

- If a Skill does not need inner NAS/NGAP fields, it passes the original Host-authorized `artifactRef` to the relevant business Tool. No Codec Tool is invoked.
- If a Skill needs protocol fields, it calls the corresponding Codec Tool exported by the `ca-client-tools` Tool Plugin through the common Runtime Action Boundary, after Skill/Run binding.
- The Codec Tool returns a structured JSON projection. The original N1/N2 artifact remains unchanged and available for forwarding or response composition.
- Encode, when needed, is also a Tool operation and returns an output artifact for Host-controlled delivery.

The Host Skill Resolver and CA-SL authoring contract are CA-SL **1.6.0 only**:

- `when` is declared only in `assurance/contract.yaml`; it is not a `SKILL.md` front-matter field.
- The Host Resolver owns continuation lookup, deterministic `when` evaluation, candidate selection and Skill binding.
- No v1alpha5 compatibility, aliases, fallback parser, mixed-version negotiation or automatic migration is supported.
- Execution mode is chosen only after exact Skill binding.

## 4. Event identity, Run and response obligations

- `externalRequestId` is optional. In the controlled Demo peer it is populated only from the configured private `X-CA-Request-Id` header; it is not a 3GPP OpenAPI field. If a real peer supplies no stable identity, it is omitted. The Plugin never synthesizes an identity from `smContextRef`, Content-ID, principal, HTTP/2 stream ID or IPC `messageId`.
- `responseChannelId` is a Plugin-local handle for the current HTTP/2 stream.
- `eventId` identifies the normalized Event instance.
- Host owns Run ID, deduplication, continuation association and `responseObligationId`. `ingress.ack` returns only `eventId`, disposition and safe diagnostics.
- `responseObligationId` is received only transiently in `delivery.dispatch`; it is not stored on the waiter, persisted, forwarded, cached or returned.
- `delivery.result` reports `delivered`, `failed` or `unknown`; an ambiguous delivery does not change the Owner's business outcome.
- Host supplies Create 201 status, `Location` and body; the Plugin does not invent the resource URI or infer business commitment.

## 5. Host-owned N1/N2 Artifact Handoff

The Host creates inbound/outbound staging directories and passes them in `host.initialize`. The Plugin writes each raw inbound part under the inbound root as `event-<eventId>/<slot file>` and sends a relative `stagingPath`, `slot`, `mediaType`, Content-ID when present and `byteLength` in `ingress.event`. Host validates the file and imports it into its ArtifactStore; any `artifactRef` stays Host-internal.

For outbound delivery, Host stages only authorized N1/N2 bytes for the exact response obligation and sends `delivery.dispatch` with `deliveryId`, `responseObligationId`, `responseChannelId` and outbound descriptors. The Plugin uses only the current `responseChannelId` waiter, reads exactly the declared files, verifies path containment and `byteLength`, constructs the SBI multipart/Content-ID response and returns `delivery.result`. It never requests arbitrary ArtifactStore reads and never sends Base64 or raw bytes over IPC.

### 5.1 OpenAMSM slot vocabulary

The slot vocabulary follows F05 §10 plus the `n2Ext1.request` completion required by F06 §4.1 (P2-01):

| Slot | Direction | Meaning |
|---|---|---|
| `n1.request` | Inbound | Original 5GSM NAS request |
| `n2.request` | Inbound | N2 SM information when the actual SBI request contains it |
| `n2Ext1.request` | Inbound | N2 SM information Ext1 (`n2SmInfoExt1.contentId`) when declared |
| `n1.confirmation` | Inbound | N1 continuation, including Release Complete (0xD4) |
| `n2.confirmation` | Inbound | N2 continuation, including Resource Release Response |
| `n1.response` | Outbound | Owner/Host-produced N1 response for the recorded obligation |
| `n2.response` | Outbound | Owner/Host-produced N2 response for the recorded obligation |

NAS media type: `application/vnd.3gpp.5gnas`. NGAP/N2 media type: `application/vnd.3gpp.ngap`.

Establish carries the original `n1.request`; `n2.request` is declared only if the actual CreateSMContext request contains that part. Modify carries the original PDU Session Modification Request (0xC9). UE-requested Release carries the original PDU Session Release Request (0xD1). Release continuation may carry N1 Release Complete (0xD4) and N2 PDU Session Resource Release Response; Host must correlate both to the exact original Run, SUPI, PSI and session generation.

The continuation Ext1 slot has no frozen contract. The ingress fails closed with `N2_EXT1_CONTINUATION_UNSPECIFIED`; this sub-item is **NOT_RUN** and must not be silently dropped or merged into `n2SmInfo`.

## 6. Tool Provider and Owner boundary

ca-client-tools v1.2.0 is the Tool Plugin implementation. It publishes the canonical Tool Interface Package and Provider runtime bindings. The OpenAMSM Event Plugin does not copy its ToolCatalog, Capability catalog, callable IDs, private backend mappings, schemas, Codec implementation or Owner operations.

The current ca-client-tools executable uses provider/v2. ca-agent-next's single Plugin Manager owns the Tool extension adapter that translates the common Runtime Action invocation to provider/v2 and maps its result back. This is a transport adapter inside the one Manager, not a second Provider Manager or Runtime.

Runtime remains the only authority for Skill-scoped Capability authorization, Action validation and dispatch, READ Observation provenance, Owner outcome/Receipt/Commit Proof interpretation, and UNKNOWN/Reconcile semantics. The Plugin cannot call the Owner directly, grant itself access or turn an HTTP 2xx into COMMITTED.

## 7. Codec T0/T2 ownership boundary

### 7.1 T0 pre-selection classifier

- The Plugin needs only the narrow 5GSM discriminator for shared `UpdateSmContext`.
- The API is library-level, owned by `ca-client-tools`, not an Agent-callable Tool, Capability, Provider binding, second Plugin or worker.
- It validates the F06 minimum header contract (at least 4 bytes, EPD `0x2e` at offset 0, message type at offset 3) and returns only `0xC9`/`0xD1`/`0xD4` or explicit malformed/unsupported.
- The Go package, API signature and version must be frozen and published by `ca-client-tools#44`. This consumer does not guess a function name/module version, copy a parser, call full `DecodeNAS` or create a second worker.
- Until then production classification is `BLOCKED_EXTERNAL_INPUT`. The implementation's production adapter always returns that block; the test-only double implements only the four-byte header contract for mapping tests.

### 7.2 T2 on-demand Codec Tool

T2 Decode/Encode operations are ordinary Tool Callables owned by the `ca-client-tools` Tool Plugin. A selected Skill invokes them only after Skill/Run binding through the Runtime Action Boundary. The Event Plugin does not call a second codec worker or duplicate codec logic; it remains responsible for SBI framing, multipart parsing, Content-ID association and raw byte handoff.

## 8. Migration boundary

| Existing ca-agent-demo responsibility | Destination |
|---|---|
| Nsmf/Namf HTTP/2 listener, HTTP wire parsing, multipart/Content-ID, raw N1/N2 extraction/staging, minimal NAS event discriminator and SBI response encoding | ca-openamsm-plugin |
| External protocol normalization and Event Schema | ca-openamsm-plugin |
| Plugin lifecycle, common IPC, Event Schema validation and dispatch | ca-agent-next Plugin Manager |
| Skill import, SkillCatalog, when evaluation and Skill selection | ca-agent-next Host / Resolver |
| Run identity, replay, response obligations, Nsmf lifecycle and resource mapping | ca-agent-next Host / Resident / Runtime |
| HostArtifactStore and private artifact access | ca-agent-next Host / Runtime |
| Tool Interface Package, OpenAMSM callable operations, Codec Tool operations when published, private bindings, Owner client, Receipt, Commit Proof and Reconcile | ca-client-tools + Host/Runtime |
| OpenAMSM SM Context identity and authoritative state | OpenAMSM Owner / SMF |
| Establish / Modify / Release business procedure | ca-skills |

NsmfRunExecutor, Resident lifecycle, RunStore, resource mapping, Owner claims, MAS Callable mapping, ToolCatalog, Owner client, Receipt/Commit/Reconcile and CA-SL Runtime remain outside this repository.

## 9. Dependency and evidence status (2026-11-28)

| Dependency | Current verified state | Consequence |
|---|---|---|
| ca-agent-next Plugin Package/IPC v1alpha1 | Merged normative contract; current F05/F06/SR-009 candidate is PR #115 Head `ba5354c` (Draft, not main) | Build only against the pinned candidate and merged IPC contract |
| ca-agent-next PR #115 | OPEN / Draft / MERGEABLE; 3/3 CI SUCCESS at `ba5354c`; not merged | The central snapshot is not on `main`; every report pins `ba5354c` |
| ca-client-tools PR #39 | OPEN / non-Draft; Phase B FINAL_PASS / PHASE_C_READY; Phase C consumer migration NOT_STARTED | Does not publish the T0 classifier API or the T2 Agent-callable Codec contract |
| ca-client-tools Issue #44 | OPEN; requests the narrow T0 5GSM classifier API | Production AC-009-04 remains `BLOCKED_EXTERNAL_INPUT` |
| ca-client-tools v1.2.0 | Provider v2 / Interface Package implementation | Consume only through the single Next Plugin Manager adapter |
| This repository PR #1 | OPEN / Draft; design documentation only, Head `5bb987a` | Design baseline; not an implementation PR |
| This repository Issue #2 / implementation branch | Implementation tracking; Go module, tests, CI | Module evidence only; not Host/system acceptance |
| Next Python Host / ArtifactStore / Channel | NOT_IMPLEMENTED for the required OpenAMSM path | Module tests use a test Host; real Host integration is a later SR-010 gate |
| OpenAMSM real Create/Modify/UE Release E2E | NOT_RUN | Tracked by SR-011 |

No Production Qualification is claimed.

## 10. Acceptance sequence and current position

1. Use the merged Plugin Package/IPC v1alpha1 contract and the pinned F05/F06/SR-009 candidate; no legacy IPC or compatibility path.
2. Implement the Go Event/Delivery Plugin independently with a test Host and protocol fixtures. Module work does not wait for Echo, Next Python Host, Owner or Provider.
3. Implement Create JSON/multipart, Content-ID/slot mapping (including optional `n2Ext1.request`), raw N1/N2 staging, three normalized Event declarations, HTTP/2 responseChannel waiters and Delivery serialization. **Done and tested at module level.**
4. Keep the production UpdateSMContext classifier adapter fail-closed until `ca-client-tools#44` publishes its exact API; test doubles cover Event mapping only. **Production binding BLOCKED_EXTERNAL_INPUT.**
5. Keep T2 full Codec Tool consumption separate. **Not this module.**
6. Report each AC separately with test commands and counts. **See [AC-009 evidence](evidence/AC-009-verification.md).** Host Run/dedup/responseObligation, Next ArtifactStore/Channel, Owner Receipt/Proof/Reconcile and real NF E2E remain outside this module.
7. Run real isolated OpenAMSM Create → Modify → UE-requested Release only under SR-011 after the required artifacts are available; do not claim Production Qualification.

The new Plugin is the second independent business used to prove zero Core business coupling. No separate third Echo-like business is required after OpenAMSM.