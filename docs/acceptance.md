# OpenAMSM Plugin acceptance

**Status:** module implementation IMPLEMENTED; module AC tests PASS on the implementation branch; production T0 classifier binding `BLOCKED_EXTERNAL_INPUT`; Next Host integration and real E2E NOT_RUN.
**Normative contract:** ca-agent-next Plugin Package / Plugin IPC v1alpha1 and F05 staging. Central design pinned to Next PR #115 Head `ba5354c575394295cfa7a790922e02422b0c96f4`; plugin design pinned to this repository PR #1 Head `5bb987aec6a643d453e7d2f111c04c4f02dbcd2f`.
**Evidence:** [AC-009-verification](evidence/AC-009-verification.md).
**Scope:** Demo-level protocol adapter. No Production Qualification claim.

## 1. Module gates (required for this repository)

Module gates are satisfied by the test Host / test peer in this repository. They do not require Echo, Next Python Host, Owner, Provider or a real NF.

| ID | Gate | Required evidence | Current state |
|---|---|---|---|
| OAP-01 | Package conformance | `plugin.yaml` validates against `connectionagent.io/plugin-package/v1alpha1`; protocol is `connectionagent.io/plugin-ipc/v1alpha1`; closed schemas and slot declarations | PASS (module) |
| OAP-02 | Lifecycle and IPC | `host.initialize`/`plugin.ready` handshake, envelope, correlation, error object, `ingress.event`/`ingress.ack`, `delivery.dispatch`/`delivery.result`, cancel and shutdown; fail-closed negatives | PASS (module) |
| OAP-03 | Skill-first boundary | Plugin emits normalized Events only; no Skill ID, execution mode, Run identity or Owner call | PASS (module) |
| OAP-04 | Event mapping | Three F06 §4.1 Event types, exact operationIds, verbatim OpenAPI request object, discriminator projection and Content-ID/slot mapping (including `n2Ext1.request`) | PASS (module, test double) |
| OAP-05 | External request identity | `externalRequestId` only from the configured trusted header; never synthesized from path/Content-ID/stream/messageId | PASS (module) |
| OAP-06 | Response channel / obligation boundary | Each dispatch maps to one live `responseChannelId`; obligation received transiently and never persisted/forwarded/returned; triple identity negative cases | PASS (module) |
| OAP-07 | Inbound binary handoff | Exact N1/N2 bytes under Host-supplied inbound staging; relative path/slot/mediaType/Content-ID/byteLength; no `artifactRef`; `ingress.ack` allowlist | PASS (module) |
| OAP-08 | Outbound binary handoff | Host-staged output descriptors only; multipart/Content-ID serialization; path containment and byteLength verification; no Base64 and no plugin artifact database | PASS (module) |
| OAP-09 | Release continuation boundary | `0xD4` and the three N2 completion responses classify as continuation and preserve original bytes/slots; Ext1 continuation has no frozen slot and fails closed (NOT_RUN) | PASS (module) / Ext1 continuation NOT_RUN |
| OAP-10 | Tool boundary | No Owner call, no ToolCatalog, no second Codec implementation | PASS (static, module) |
| OAP-13 | Zero Core coupling | Plugin is an independent Go process; no Agent Core/Skill Resolver/Runtime/Plugin Manager change required by this implementation | PASS (static, module) |
| OAP-15 | Schema ownership | Request/response Schemas are local projections of the fixed TS 29.502 V19.5.0 semantics; the original 3GPP YAML is not modified | PASS (module) |
| OAP-16 | T0 minimal classifier (test seam) | Module tests use an explicitly test-only double for mapping; production adapter returns `BLOCKED_EXTERNAL_INPUT` and never parses NAS | PASS (module) / production **BLOCKED_EXTERNAL_INPUT** |

## 2. System / phase-only gates (not module prerequisites)

These gates belong to Host/system composition or later SR phases. They MUST NOT be used to block or promote this module's acceptance.

| ID | Gate | Owner phase | Why it is not a module gate |
|---|---|---|---|
| OAP-11 | Codec qualification | SR-026 / T2 Codec Tool publication | The Event Plugin never invokes Codec Tools; NAS/N2 qualification is a separate Tool contract |
| OAP-12 | Real Create/Modify/Release | SR-011 system E2E | Requires Next Python Host, real isolated OpenAMSM Owner, Provider and Skill artifacts |
| OAP-14 | On-demand Codec | Skill/Runtime phase | A selected Skill invokes the Codec Tool after Skill/Run binding; the Event Plugin only hands off raw artifacts |
| OAP-16 production binding | `ca-client-tools#44` published API | External owner | Blocked until the exact package/API/version is merged, published and pinned |

## 3. Protocol and artifact fixtures

- CreateSMContext request (JSON part + N1/N2 parts) and a 201/Location response with an outbound N1 response.
- UpdateSMContext carrying PDU Session Modification Request (`0xC9`) and UE-requested Release Request (`0xD1`).
- Release continuation with Release Complete (`0xD4`) and N2 Resource Release Response, separately and together.
- N2-only continuation for the three completion responses.
- `n2SmInfoExt1` with `n2SmInfoTypeExt1` on Create and Update (optional slot).
- Multipart Content-ID present/absent, duplicate, unresolved, unreferenced, wrong media type, duplicate reference.
- Per-stream `responseChannelId` under concurrent HTTP/2 requests; unknown channel, wrong obligation, wrong event type, second dispatch, one correlated result per dispatch.
- Inbound/outbound staging path traversal, absolute path, symlink escape, missing file, empty part and byte-length mismatch.
- Update routing fixtures for `0xC9`/`0xD1`/`0xD4` plus malformed/unknown/unsupported discriminator.
- Production adapter blocked; test-only double mapping.
- Demo retry fixtures with the configured `X-CA-Request-Id`: the Plugin creates independent channels; Host-owned deduplication is out of module scope.
- Delivery failed/unknown without changing Owner business outcome.

## 4. Module vs system boundary

- Plugin module scope: OAP-01/02/03/04/05/06/07/08/09/10/13/15 and the OAP-16 test seam.
- Host-owned scope: stable request deduplication, `responseObligationId` creation/expiry, original Run association, continuation index, generation/transaction validation, ArtifactStore import and opaque `artifactRef` authorization.
- System scope: real Create → Modify → UE-requested Release, Owner Receipt/Commit Proof, UNKNOWN/Reconcile and zero duplicate mutation remain SR-011 / system E2E gates.
- Do not promote any Host/system gate to Plugin module PASS. Test Host success does not prove Next Python Host, Provider or real NF integration.

## 5. Non-goals

Do not add:

- a Plugin-local IPC or compatibility protocol;
- Generic Agent default without a selected Skill;
- a separate Provider Manager, Tool registry, Event Registry or business Contract Package;
- direct Owner calls or duplicated MAS Tool implementation;
- Plugin-owned Artifact database, Base64 binary transport or remote Artifact service;
- extra SHA-256, content digest, package signature or Trust Registry;
- legacy compatibility, Marketplace, automatic downloads, hot reload or production-grade DFX.

Keep basic manifest/Schema/version/reference validation, path containment, explicit process configuration, useful logs and the minimum correlation required by the actual SBI operation.