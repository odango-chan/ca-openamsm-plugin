# OpenAMSM Plugin acceptance

**Status:** design-only; implementation NOT_STARTED; acceptance NOT_RUN.
**Normative contract:** ca-agent-next Plugin Package v1alpha1 / Plugin IPC v1alpha1 and the current F05 staging rules, pinned in [Next PR #115 integration snapshot](https://github.com/odango-chan/ca-agent-next/blob/39e0bb018e8cd1cc282da3853636c5666dd9a86b/docs/%E8%A7%84%E8%8C%83/%E6%8F%92%E4%BB%B6%E5%8C%85%E4%B8%8E%E8%BF%9B%E7%A8%8B%E9%80%9A%E4%BF%A1%E5%8D%8F%E8%AE%AE%E8%A7%84%E8%8C%83.md). This repository owns no independent IPC or Artifact protocol.
**Scope:** Demo-level protocol adapter; no Production Qualification claim.

## 1. Required gates

| ID | Gate | Required evidence |
|---|---|---|
| OAP-01 | Package conformance | plugin.yaml validates against connectionagent.io/plugin-package/v1alpha1; process.protocol is connectionagent.io/plugin-ipc/v1alpha1. No ca-agent-host.plugin/v0 alias or fallback. |
| OAP-02 | Lifecycle and IPC | host.initialize/plugin.ready handshake, common envelope, correlation, error object, ingress.event/ack, delivery.dispatch/result, cancellation and shutdown follow the Next contract. |
| OAP-03 | Skill-first boundary | Plugin emits normalized Event only. Host binds a Skill before execution; Plugin supplies no Skill ID or execution mode. No Generic Agent fallback without a Skill. |
| OAP-04 | Event mapping | Implement the three F06 v0.3.7 normalized Event types, exact operationIds, original OpenAPI request object, discriminator projection and Content-ID/slot mapping. Create is `PostSmContexts`; Modify and UE-requested Release share `UpdateSmContext`; N1 `0xD4` / the three N2 responses are continuation. No Skill selection or Run association in Plugin. |
| OAP-05 | External request identity | `externalRequestId` is optional and is populated only when the trusted peer supplies a stable identity (controlled Demo: configured `X-CA-Request-Id`). It is not a 3GPP field. No canonical ID may be synthesized from `smContextRef`, Content-ID, principal, HTTP/2 stream ID or IPC messageId. Host owns deduplication; absent real-peer identity keeps cross-reconnect replay E2E BLOCKED/NOT_RUN. |
| OAP-06 | Response channel / obligation boundary | Plugin maps each `delivery.dispatch` to the matching live `responseChannelId` waiter; it transiently receives `responseObligationId` but never persists, forwards or returns it. `delivery.result` returns `deliveryId` / disposition / optional statusCode / safe diagnostics only. Host alone creates response obligations, deduplicates retries and associates the original Run. |
| OAP-07 | Inbound binary handoff | Plugin writes exact N1/N2 parts under Host-supplied inbound staging and sends relative path/slot/mediaType/Content-ID/byteLength descriptors. Host validates and imports bytes into ArtifactStore. `ingress.ack` returns only `eventId`, disposition and safe diagnostics; Host `artifactRef` never leaves Host. |
| OAP-08 | Outbound binary handoff | Host stages only authorized N1/N2 output for the exact response obligation and dispatches descriptors with `deliveryId`, `responseObligationId` and `responseChannelId`. Plugin serializes multipart/Content-ID and returns `delivery.result` without echoing Host identities. No Base64 or Plugin Artifact database. |
| OAP-09 | Release continuation boundary | Plugin classifies N1 `0xD4` / applicable N2 response as continuation Event and preserves original bytes/slots. Host continuation index and Owner/Runtime validate exact original Run/SUPI/PSI/generation/transaction; Plugin never receives or stores those identities. Exact Run resumption is a Host/SR-011 system gate, not a Plugin module PASS. |
| OAP-10 | Tool boundary | Plugin never calls OpenAMSM Owner or duplicates ToolCatalog/Owner bindings. All Tool Actions, including any published Codec Tool operations, go through the ca-client-tools Tool Plugin and Host Runtime Action Boundary. No separate Codec Plugin/Manager is introduced. |
| OAP-11 | Codec qualification | Use only merged/released Codec Tool API and explicitly qualified NAS/N2 subset. NAS currently has 8 qualified types; N2 has 7 scoped qualified roots; NGAP full-PDU qualification is zero. Callable-but-unqualified inputs fail qualification checks. The current pure `protocol/nas5gsm` library is not Agent-callable until exported in the canonical Interface Package. |
| OAP-12 | Real Create/Modify/Release | One UE/PSI completes Establish → Modify → UE-requested Release against real isolated OpenAMSM Owner through the new Host/Plugin/Provider path, preserving N1/N2 and proving Owner outcomes and zero duplicate mutation. |
| OAP-13 | Zero Core coupling | Adding this Plugin, its Event Schemas, Skills and Tool package requires no OpenAMSM-specific code in Agent Core, Skill Resolver, Runtime or Plugin Manager. |
| OAP-14 | On-demand Codec | A Skill that does not need protocol fields passes the original artifactRef to the business Tool without invoking Codec. A Skill that needs fields invokes the Codec Tool through the same Runtime Action Boundary. Decode returns structured JSON and never replaces or mutates the original N1/N2 artifact. |
| OAP-15 | Schema ownership | SBI request/response and multipart slots follow the applicable 3GPP OpenAPI. NAS/NGAP structures follow the relevant 3GPP definitions. Tool Interface Package defines Codec IO; Skill requestSchema defines only business input and does not duplicate the full protocol Schema. |
| OAP-16 | T0 minimal classifier | Production N1 classification uses the narrow library-level T0 5GSM discriminator API published by `ca-client-tools#44`; it returns only `0xC9` / `0xD1` / `0xD4` or explicit malformed/unsupported. It is not an Agent Tool and does not parse IEs. Until package/API/version are published, test doubles may cover Event mapping but production AC-009-04 remains BLOCKED_EXTERNAL_INPUT. No local parser or full `DecodeNAS` shortcut. |

## 2. Required protocol and artifact fixtures

- CreateSMContext request and 201/Location response.
- UpdateSMContext carrying PDU Session Modification Request (0xC9).
- UpdateSMContext or the verified operation carrying UE-requested PDU Session Release Request (0xD1).
- Release continuation with PDU Session Release Complete (0xD4) and N2 PDU Session Resource Release Response.
- Multipart Content-ID present/absent cases according to the actual OpenAMSM OpenAPI.
- Per-stream `responseChannelId` mapping under concurrent HTTP/2 requests; unknown/closed channel and wrong `deliveryId` negative cases.
- Demo peer retry fixtures using `X-CA-Request-Id` may be used for Host integration only; same ID/different content conflict and new response-obligation attachment are Host-owned, not Plugin deduplication tests.
- Inbound/outbound Artifact path traversal, symlink, missing file and byte-length mismatch rejection.
- Delivery failed/unknown without changing Owner business outcome.
- Codec negative cases for callable but unqualified NAS/N2/NGAP inputs.
- UpdateSMContext routing fixtures for NAS 0xC9 versus 0xD1, plus malformed/unknown/unsupported discriminator cases.
- A no-decode Skill path proving no Codec Tool invocation occurs.
- A decode-required Skill path proving the Codec Tool is invoked through the normal Runtime Action Boundary and the original artifact remains available.

Exact operationIds, part names, field mappings and required/optional slot declarations must be frozen from OpenAMSM OpenAPI and current Host ingress before these fixtures are implemented. Do not guess them.

## 3. External dependencies and current evidence (2026-11-28)

| Dependency | Current verified state | Consequence |
|---|---|---|
| ca-agent-next Plugin Package/IPC v1alpha1 | Merged normative contract; current F05/F06/SR-009 candidate is in PR #115 integration branch (Draft, not main) | Implement only against the pinned central candidate and merged IPC contract. |
| ca-agent-next PR #115 | OPEN / Draft; current mainline integration candidate | Central SR/F05/F06 snapshot is not yet merged to main; pin the reviewed SHA in every report. |
| ca-client-tools PR #39 | OPEN / non-Draft; Phase B FINAL_PASS / PHASE_C_READY; Phase C consumer migration NOT_STARTED | Does not by itself publish the T0 classifier API or the T2 Agent-callable Codec Tool contract. |
| ca-client-tools Issue #44 | OPEN; request to publish narrow T0 5GSM classifier API | Production AC-009-04 remains BLOCKED until exact Go package/API/version and tests are merged/published. |
| ca-client-tools v1.2.0 | Provider v2 / Interface Package implementation | Consume only through the single Next Plugin Manager adapter; no second Provider Manager or direct Owner client. |
| ca-openamsm-plugin PR #1 | OPEN / Draft; design documentation only | No Go runtime, SBI listener migration, Artifact Handoff implementation or module acceptance is claimed. |
| Next Python Host / ArtifactStore / Channel | NOT_IMPLEMENTED for the required OpenAMSM path | Plugin module tests use a test Host; real Host integration is a later system gate. |
| OpenAMSM real Create/Modify/UE Release E2E | NOT_RUN | Tracked by SR-011 after required Host/Owner/Provider/Skill artifacts are ready. |

No Production Qualification is claimed.

## 4. Module versus system acceptance boundary

- Plugin module scope: OAP-01/02/03/04 Event mapping and schemas/07/08 staging and Delivery/05 optional identity pass-through/06 live response-channel mapping/16 test seam. Production OAP-16 remains BLOCKED until ca-client-tools#44 publishes the exact API.
- Host-owned scope: stable request deduplication, `responseObligationId` creation/expiry, original Run association, continuation index, generation/transaction validation, ArtifactStore import and opaque `artifactRef` authorization.
- System scope: real Create → Modify → UE-requested Release, Owner Receipt/Commit Proof, UNKNOWN/Reconcile and zero duplicate mutation remain SR-011 / system E2E gates.
- Do not promote any Host/system gate to Plugin module PASS. Test Host success does not prove Next Python Host, Provider or real NF integration.

## 4. Non-goals

Do not add:

- a Plugin-local IPC or compatibility protocol;
- Generic Agent default without a selected Skill;
- separate Provider Manager, Tool registry, Event Registry or business Contract Package;
- direct Owner calls or duplicated MAS Tool implementation;
- Plugin-owned Artifact database, Base64 binary transport or remote Artifact service;
- extra SHA-256, content digest, package signature or Trust Registry;
- legacy compatibility, Marketplace, automatic downloads, hot reload or production-grade DFX.

Keep basic manifest/Schema/version/reference validation, path containment, explicit process configuration, useful logs and the minimum correlation required by the actual SBI operation.
