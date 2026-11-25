# OpenAMSM Plugin acceptance

**Status:** design-only; implementation NOT_STARTED; acceptance NOT_RUN.
**Normative contract:** ca-agent-next Plugin Package v1alpha1 and Plugin Artifact Handoff v1alpha1. This repository owns no independent IPC or Artifact protocol.
**Scope:** Demo-level protocol adapter; no Production Qualification claim.

## 1. Required gates

| ID | Gate | Required evidence |
|---|---|---|
| OAP-01 | Package conformance | plugin.yaml validates against connectionagent.io/plugin-package/v1alpha1; process.protocol is connectionagent.io/plugin-ipc/v1alpha1. No ca-agent-host.plugin/v0 alias or fallback. |
| OAP-02 | Lifecycle and IPC | host.initialize/plugin.ready handshake, common envelope, correlation, error object, ingress.event/ack, delivery.dispatch/result, cancellation and shutdown follow the Next contract. |
| OAP-03 | Skill-first boundary | Plugin emits normalized Event only. Host binds a Skill before execution; Plugin supplies no Skill ID or execution mode. No Generic Agent fallback without a Skill. |
| OAP-04 | Event mapping | CreateSMContext and UpdateSMContext operationIds and normalized JSON fields are verified against current OpenAMSM OpenAPI. For shared UpdateSMContext, the Event Plugin extracts only the minimal NAS discriminator needed to distinguish Modify from UE-requested Release; this is protocol-to-Event normalization, not Skill selection or full decode. The Host Resolver selects the Skill. UE-requested Release is not confused with AMF/network-initiated ReleaseSMContext. |
| OAP-05 | Stable request identity | externalRequestId is derived from a stable protocol identity or a documented/tested canonical request identity. HTTP/2 stream ID and IPC messageId are never used as logical request identity. |
| OAP-06 | Retry and response obligation | Same request retry attaches a new responseObligationId to the original Run; conflicting reuse fails closed; Plugin maps delivery to the correct live response channel. |
| OAP-07 | Inbound binary handoff | Original N1/N2 multipart parts are written under Host-supplied inbound staging, declared with slot/mediaType/Content-ID/relative path/byteLength, imported by Host ArtifactStore and acknowledged with Host-issued artifactRefs. |
| OAP-08 | Outbound binary handoff | Host stages only authorized N1/N2 output for the exact responseObligationId; Plugin serializes multipart/Content-ID and reports delivery.result. No Base64 or Plugin Artifact database. |
| OAP-09 | Release continuation | N1 PDU Session Release Complete (0xD4) and N2 PDU Session Resource Release Response, where applicable, resume the exact original Run/SUPI/PSI/generation. Stale/duplicate messages cannot wake a later Run. |
| OAP-10 | Tool boundary | Plugin never calls OpenAMSM Owner or duplicates ToolCatalog/Owner bindings. All Tool Actions, including any published Codec Tool operations, go through the ca-client-tools Tool Plugin and Host Runtime Action Boundary. No separate Codec Plugin/Manager is introduced. |
| OAP-11 | Codec qualification | Use only merged/released Codec Tool API and explicitly qualified NAS/N2 subset. NAS currently has 8 qualified types; N2 has 7 scoped qualified roots; NGAP full-PDU qualification is zero. Callable-but-unqualified inputs fail qualification checks. The current pure `protocol/nas5gsm` library is not Agent-callable until exported in the canonical Interface Package. |
| OAP-12 | Real Create/Modify/Release | One UE/PSI completes Establish → Modify → UE-requested Release against real isolated OpenAMSM Owner through the new Host/Plugin/Provider path, preserving N1/N2 and proving Owner outcomes and zero duplicate mutation. |
| OAP-13 | Zero Core coupling | Adding this Plugin, its Event Schemas, Skills and Tool package requires no OpenAMSM-specific code in Agent Core, Skill Resolver, Runtime or Plugin Manager. |
| OAP-14 | On-demand Codec | A Skill that does not need protocol fields passes the original artifactRef to the business Tool without invoking Codec. A Skill that needs fields invokes the Codec Tool through the same Runtime Action Boundary. Decode returns structured JSON and never replaces or mutates the original N1/N2 artifact. |
| OAP-15 | Schema ownership | SBI request/response and multipart slots follow the applicable 3GPP OpenAPI. NAS/NGAP structures follow the relevant 3GPP definitions. Tool Interface Package defines Codec IO; Skill requestSchema defines only business input and does not duplicate the full protocol Schema. |

## 2. Required protocol and artifact fixtures

- CreateSMContext request and 201/Location response.
- UpdateSMContext carrying PDU Session Modification Request (0xC9).
- UpdateSMContext or the verified operation carrying UE-requested PDU Session Release Request (0xD1).
- Release continuation with PDU Session Release Complete (0xD4) and N2 PDU Session Resource Release Response.
- Multipart Content-ID present/absent cases according to the actual OpenAMSM OpenAPI.
- Same externalRequestId/same payload retry; same ID/different payload conflict.
- New HTTP/2 response channel attached to an existing Run.
- Inbound/outbound Artifact path traversal, symlink, missing file and byte-length mismatch rejection.
- Delivery failed/unknown without changing Owner business outcome.
- Codec negative cases for callable but unqualified NAS/N2/NGAP inputs.
- A no-decode Skill path proving no Codec Tool invocation occurs.
- A decode-required Skill path proving the Codec Tool is invoked through the normal Runtime Action Boundary and the original artifact remains available.

Exact operationIds, part names, field mappings and required/optional slot declarations must be frozen from OpenAMSM OpenAPI and current Host ingress before these fixtures are implemented. Do not guess them.

## 3. External dependencies and current evidence

| Dependency | State on 2026-11-25 | Acceptance consequence |
|---|---|---|
| ca-agent-next PR #1 | OPEN; documentation-only proposed normative contract | Freeze/merge the contract before implementation. |
| ca-skills PR #88 | OPEN; same-identifier CA-SL v1alpha5 refresh | Do not claim the refreshed when semantics are published until merged. |
| ca-skill-compiler v2.1.0 | Released; new when checked-program Artifact support not established | Assurance source import and when selection remain blocked until Compiler support is verified/released. SKILL.md-only import is Host-owned and bypasses Compiler. |
| ca-client-tools v1.2.0 | Released Tool Plugin package and Provider v2 process | Consume through the Next Plugin Manager adapter. Real-NF SBI multipart Provider binding remains NOT_VERIFIED; Codec operations are callable only after they are published in the canonical Interface Package. |
| ca-client-tools PR #39 | OPEN; Phase B final pass; Phase C consumer migration NOT_STARTED | Do not claim merged/released Codec or full NGAP support. |
| ca-agent-demo PR #246 | OPEN; synthetic Owner G5 22 passed; real G6 NOT_RUN/BLOCKED; G7 PARTIAL | Synthetic evidence is not a real OpenAMSM E2E pass. |

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
