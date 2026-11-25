# OpenAMSM Go Plugin — Deployment Adapter Design

**Status:** design-only proposal aligned to ca-agent-next. No Go runtime, SBI listener migration, Artifact Handoff implementation or E2E acceptance is claimed.

**Normative Host contract:** [ca-agent-next PR #1](https://github.com/odango-chan/ca-agent-next/pull/1), specifically:
- [Plugin Package v1alpha1 and common IPC](https://github.com/odango-chan/ca-agent-next/blob/docs/architecture-baseline-20261125/docs/plugin-package.md)
- [Host-owned Plugin Artifact Handoff](https://github.com/odango-chan/ca-agent-next/blob/docs/architecture-baseline-20261125/docs/plugin-artifact-handoff.md)
- [Skill-first architecture](https://github.com/odango-chan/ca-agent-next/blob/docs/architecture-baseline-20261125/docs/architecture.md)
- [OpenAMSM acceptance gates](https://github.com/odango-chan/ca-agent-next/blob/docs/architecture-baseline-20261125/docs/acceptance.md)

This repository does not define another Plugin API, IPC version, Skill selector, Artifact service or Tool contract.

## 1. Purpose and strict boundary

ca-openamsm-plugin is an independent Go process that adapts OpenAMSM external SBI protocol ingress and response delivery to the generic CA Agent Host Plugin contract.

It owns:

- external Nsmf/Namf HTTP/2 SBI listener and wire protocol handling;
- multipart parsing and Content-ID association;
- protocol-to-normalized-Event mapping;
- raw N1/N2 binary extraction and Host Artifact Handoff; minimal NAS discriminator extraction only when required for Event normalization; preserve original bytes without eager deep decoding;
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

The Plugin MUST implement the exact native contract defined by ca-agent-next Plugin Package v1alpha1:

- manifest apiVersion: connectionagent.io/plugin-package/v1alpha1;
- process protocol: connectionagent.io/plugin-ipc/v1alpha1;
- YAML plugin.yaml;
- extensions event.ingress and event.delivery;
- one long-lived stdio NDJSON process;
- the common message envelope, correlation rules, error shape and lifecycle.

Do not use ca-agent-host.plugin/v0. Do not add an alias, compatibility adapter or Plugin-local message protocol. The old ca-agent-demo Plugin Contract v0 and PR #249 are historical design inputs only, not the target authority.

The Plugin is an ordinary standalone Go executable, not Go's in-process plugin/.so mechanism. Host starts it using an operator-approved argv without shell interpretation. stdout is reserved for IPC; diagnostics go to stderr. Concurrent request handling must serialize writes to stdout and preserve message correlation.

### 2.1 Manifest target

The following is an illustrative shape; final field validation is owned by ca-agent-next Plugin Package v1alpha1.

~~~yaml
apiVersion: connectionagent.io/plugin-package/v1alpha1
kind: Plugin
metadata:
  id: urn:connectionagent:plugin:openamsm
  version: 0.1.0

process:
  executable: bin/ca-openamsm-plugin
  args: [serve]
  protocol: connectionagent.io/plugin-ipc/v1alpha1

extensions:
  event.ingress:
    events:
      - type: nsmf.create-sm-context
        description: Normalized Nsmf CreateSMContext request.
        requestSchema: schemas/create-sm-context.request.schema.yaml
        artifacts:
          input:
            - slot: n1.request
              mediaType: application/vnd.3gpp.5gnas
              required: true
            - slot: n2.request
              mediaType: application/vnd.3gpp.ngap
              required: false
      - type: nsmf.update-sm-context
        description: Normalized Nsmf UpdateSMContext request carrying Modify or UE-requested Release.
        requestSchema: schemas/update-sm-context.request.schema.yaml
        artifacts:
          input:
            - slot: n1.request
              mediaType: application/vnd.3gpp.5gnas
              required: true
      - type: nsmf.update-sm-context.continuation
        description: Normalized continuation input for an existing UpdateSMContext Run.
        requestSchema: schemas/update-sm-context.continuation.schema.yaml
        artifacts:
          input:
            - slot: n1.confirmation
              mediaType: application/vnd.3gpp.5gnas
              required: false
            - slot: n2.confirmation
              mediaType: application/vnd.3gpp.ngap
              required: false

  event.delivery:
    responses:
      - eventType: nsmf.create-sm-context
        responseSchema: schemas/sbi-response.schema.yaml
        artifacts:
          output:
            - slot: n1.response
              mediaType: application/vnd.3gpp.5gnas
              required: false
            - slot: n2.response
              mediaType: application/vnd.3gpp.ngap
              required: false
      - eventType: nsmf.update-sm-context
        responseSchema: schemas/sbi-response.schema.yaml
        artifacts:
          output:
            - slot: n1.response
              mediaType: application/vnd.3gpp.5gnas
              required: false
            - slot: n2.response
              mediaType: application/vnd.3gpp.ngap
              required: false
      - eventType: nsmf.update-sm-context.continuation
        responseSchema: schemas/sbi-response.schema.yaml
~~~

The Event names above are normalized internal Event types. Their exact mapping to OpenAMSM OpenAPI operationIds and request fields is a required pre-implementation check. The Plugin does not add a Skill ID, Goal, business Contract Package, execution mode or authorization grant to the manifest.

If the deployment includes AMF/network-initiated ReleaseSMContext, it is a distinct protocol event and must not be conflated with the UE-requested PDU Session Release Skill. Add its normalized event only when that scope is explicitly included.

## 3. Skill-first event consumption

Every fresh business Event must bind a Skill before execution. There is no unbound Generic LLM Agent fallback.

The Plugin publishes only:
- declared normalized eventType;
- validated normalized payload;
- protocol source and stable external request identity;
- Host Artifact references after ingress acceptance.

It does not select or name a Skill. It also does not eagerly decode N1/N2 for business reasoning. It may extract only the minimum NAS message discriminator needed to normalize a shared SBI operation such as UpdateSMContext (Modify versus UE-requested Release). This is protocol-to-Event classification, not Skill selection or full business decoding. The Host Resolver still selects the Skill. Deeper protocol fields remain available through on-demand Codec Tools.

### 3.1 On-demand protocol decoding

- If a Skill does not need inner NAS/NGAP fields, it passes the original artifactRef to the relevant business Tool. No Codec Tool is invoked.
- If a Skill needs protocol fields, it calls the corresponding Codec Tool exported by the `ca-client-tools` Tool Plugin through the common Runtime Action Boundary.
- The Codec Tool returns a structured JSON projection. The original N1/N2 artifact remains unchanged and available for forwarding or response composition.
- Encode, when needed, is also a Tool operation and returns an output artifact reference for Host-controlled delivery.
- Codec operations must be published in the canonical Tool Interface Package and limited to the exact message subset that has been implemented and tested.

The Host Skill Resolver applies the CA-SL v1alpha5 rules:

1. Exact continuation is resolved first and resumes the original Run without fresh Skill selection.
2. Evaluate optional assurance/contract.yaml.when for imported Assurance Skills against normalized event.type and input.
3. One true match selects directly.
4. Multiple true matches may be disambiguated by the LLM only within that exact candidate set.
5. With zero true matches, semantic selection may use only Skills without when, using cached SKILL.md content/metadata.
6. If no candidate remains, ask for clarification or reject. Do not fall back to a generic Agent with no Skill.
7. Select Deterministic or LLM-guided mode only after exact Skill binding.

No Assurance means LLM-guided only. Valid Assurance supports Deterministic and LLM-guided modes. Invalid present Assurance is an import failure, never a silent downgrade. Both modes use the same Runtime Action Boundary.

The existing ca-skills packages are the business source:

- establish-pdu-session-mas;
- modify-pdu-session-mas;
- release-pdu-session-mas.

Create, Modify and UE-requested Release must be distinguished by normalized Event metadata and validated request fields. Modify and UE-requested Release may share an Nsmf UpdateSMContext ingress; the Resolver, not the Plugin, chooses the Skill. The exact discriminator field and mapping to OpenAMSM OpenAPI must be frozen with schema tests. Do not infer the Skill from an HTTP path alone or duplicate business selection logic in Go.

## 4. Event identity, Run and response obligations

The Plugin distinguishes logical request identity from the current HTTP/2 response stream:

- externalRequestId is the stable protocol request/retry identity, or a documented and tested canonical identity derived from immutable protocol fields.
- responseChannelId is a Plugin-local handle for the current HTTP/2 stream.
- eventId identifies the normalized Event instance.
- Run ID and responseObligationId are assigned by Host and returned in ingress.ack.

Never use HTTP/2 stream ID or IPC messageId as the logical business request identity.

A retry with the same stable externalRequestId and identical normalized payload/artifacts attaches a new response obligation to the original Run. Reuse with different normalized content or artifact bytes is a conflict. The Host owns deduplication and Run association; Plugin retains only the live protocol waiter needed to write the response.

If a stable retry identity cannot be established for an operation, that operation's replay-safe E2E gate remains NOT_RUN. Do not add an Event content digest or extra SHA-256 mechanism.

A delivery result reports delivered, failed or unknown. An ambiguous delivery remains UNKNOWN and does not change the Owner's business outcome. Create 201 and Location are serialized from Host-supplied status/headers; Plugin does not invent the resource URI or infer business commitment.

## 5. Host-owned N1/N2 Artifact Handoff

Binary data uses the exact [ca-agent-next Plugin Artifact Handoff v1alpha1](https://github.com/odango-chan/ca-agent-next/blob/docs/architecture-baseline-20261125/docs/plugin-artifact-handoff.md).

The Host creates inbound/outbound staging directories and passes them in host.initialize. The Plugin writes raw inbound N1/N2 parts under the supplied inbound root and sends relative stagingPath, slot, mediaType, Content-ID when present and byteLength in ingress.event. Host validates the file, imports it into its existing ArtifactStore, associates a Host-issued opaque artifactRef with the Event/Run and returns that reference in ingress.ack.

For outbound delivery, Host stages only the authorized N1/N2 bytes under the outbound root and sends descriptors for the exact responseObligationId. Plugin reads those files, constructs the correct SBI multipart/Content-ID response and returns delivery.result.

Rules:

- no Base64 in NDJSON;
- no Plugin-owned Artifact database or namespace;
- no arbitrary Plugin reads from Host ArtifactStore;
- no absolute paths, traversal or symlink escape;
- no extra SHA-256, content digest or package-integrity mechanism;
- Host owns Artifact custody, Run association, authorization and cleanup.

### 5.1 OpenAMSM slot vocabulary

Use the shared slot vocabulary defined by Next:

| Slot | Direction | Meaning |
|---|---|---|
| n1.request | Inbound | Original 5GSM NAS request |
| n2.request | Inbound | N2 SM information when the actual SBI request contains it |
| n1.confirmation | Inbound | N1 continuation, including PDU Session Release Complete (0xD4), when delivered as a later ingress |
| n2.confirmation | Inbound | N2 continuation, including PDU Session Resource Release Response, when delivered as a later ingress |
| n1.response | Outbound | Owner/Host-produced N1 response for the recorded obligation |
| n2.response | Outbound | Owner/Host-produced N2 response for the recorded obligation |

NAS media type: application/vnd.3gpp.5gnas. NGAP/N2 media type: application/vnd.3gpp.ngap.

Establish initially carries the original n1.request; n2.request is declared only if the actual CreateSMContext request contains that part. Modify carries the original PDU Session Modification Request (0xC9). UE-requested Release carries the original PDU Session Release Request (0xD1). Release continuation may carry N1 Release Complete (0xD4) and N2 PDU Session Resource Release Response; Host must correlate both to the exact original Run, SUPI, PSI and session generation.

The exact SBI operationIds, multipart part names, required/optional slots and response mappings must be checked against current OpenAMSM OpenAPI and the existing ca-agent-demo Host ingress before implementation. Do not guess missing mappings.

## 6. Tool Provider and Owner boundary

ca-client-tools v1.2.0 is the Tool Plugin implementation. It publishes the canonical Tool Interface Package and Provider runtime bindings. The OpenAMSM Event Plugin does not copy its ToolCatalog, Capability catalog, callable IDs, private backend mappings, schemas, Codec implementation or Owner operations.

Protocol Codec Tools, when added, belong to the same `tools` extension and canonical Interface Package. The Event Plugin is not a separate Codec Plugin or Tool Provider.

The current ca-client-tools executable uses provider/v2. ca-agent-next's one Plugin Manager owns the Tool extension adapter that translates the common Runtime Action invocation to provider/v2 and maps its result back. This is a transport adapter inside the one Manager, not a second Provider Manager or Runtime.

Runtime remains the only authority for:
- Skill-scoped Capability authorization;
- Action validation and dispatch;
- READ Observation provenance;
- Owner outcome, Receipt and Commit Proof interpretation;
- UNKNOWN and Reconcile semantics.

The Plugin cannot call the Owner directly, grant itself access or turn an HTTP 2xx into COMMITTED.

## 7. Codec Tool ownership and qualification boundary

Protocol Codec is a callable Tool capability, not an internal decoder embedded in the OpenAMSM Event Plugin.

- The `ca-client-tools` Tool Plugin owns the shared Go codec implementation and the public Codec Tool contracts.
- When the accepted codec API and Phase C consumer integration are available, expose the required NAS/NGAP/N2 Decode/Encode operations through the existing Tool Interface Package and `tools` extension.
- The selected Skill calls Decode only when it needs protocol fields. If it does not, the original artifact reference is passed to the business Tool unchanged.
- The Event Plugin remains responsible for SBI framing, multipart parsing, Content-ID association and raw byte handoff. It does not call a second codec worker or duplicate codec logic.
- The SBI operation provides the base Event type, but UpdateSMContext can represent both Modify and UE-requested Release. The Event Plugin must therefore extract only the minimal NAS discriminator needed to normalize these cases before publishing the Event. This is not a Skill-selection decision or a full PDU/IE decode. Reuse the shared codec implementation from ca-client-tools for the exact supported message subset; if that discriminator is not supported, block the integration rather than add a second parser. The Host Resolver selects the Skill from the normalized Event. A future direct NAS/NGAP ingress is a separate scope.

Reuse the shared Go codec from ca-client-tools when its public API is merged/released and Phase C consumer migration is complete. Do not copy Free5GC codec code or launch a second worker from this Plugin.

Current evidence for ca-client-tools PR #39 (OPEN, not merged):

| Namespace | Enumerated | CA tested | CA qualified |
|---|---:|---:|---:|
| NAS 5GSM | 20 | 8 | 8 |
| N2 transfer roots | 41 | 7 | 7, within evidenced scope |
| NGAP full PDU | 120 | 1 self-round-trip only | 0 |

NAS qualified message types include 0xC1, 0xC2, 0xC3, 0xC9, 0xCB, 0xD1, 0xD3 and 0xD4. N2 qualification is limited to the seven evidenced roots/IE scopes. NGAP full-PDU qualification remains zero. Byte-identical re-encode alone is not qualification.

PR #39 reports Phase B final pass, but it is OPEN and Phase C consumer migration is NOT_STARTED. Do not claim these APIs are already merged/released or claim complete NGAP support. OpenAMSM acceptance must use only the exact qualified NAS/N2 subset required by the flow and must include negative tests for callable-but-unqualified inputs.

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

Do not move NsmfRunExecutor, Resident lifecycle, RunStore, resource mapping, Owner claims, MAS Callable mapping, ToolCatalog, Owner client, Receipt/Commit/Reconcile or CA-SL Runtime into this repository. Do not move the entire integrations/fivegc directory.

## 9. Dependency and evidence status — 2026-11-25

| Dependency | Current state | Consequence |
|---|---|---|
| ca-agent-next PR #1 | OPEN; documentation-only target contract | This PR aligns to the proposed v1alpha1 contract. Do not implement against an unmerged, changing contract; freeze it first. |
| ca-skills PR #88 | OPEN; same-identifier CA-SL v1alpha5 refresh with optional Assurance and contract.when | The normative language refresh is not merged. |
| ca-skill-compiler v2.1.0 | Released; support for refreshed contract.when and the portable checked-program Artifact is not established | Assurance import/when Resolver remains blocked until Compiler owner verifies/releases the required representation. SKILL.md-only import is Host-owned and bypasses Compiler. |
| ca-client-tools v1.2.0 | Released Tool Interface Package and Provider runtime bindings; provider/v2 process | Reuse through the one Plugin Manager adapter. Real-NF SBI multipart binding through the Provider artifact channel remains NOT_VERIFIED. |
| ca-client-tools PR #39 | OPEN; Phase B final pass, Phase C consumer migration NOT_STARTED | Codec consumer integration and release are pending. NAS 8 qualified; N2 7 scoped qualified; NGAP 0 full-PDU qualified. |
| ca-agent-demo PR #246 | OPEN; synthetic Owner G5 22 passed; real isolated Scheme C G6 NOT_RUN/BLOCKED; G7 PARTIAL | Synthetic G5 is not real OpenAMSM E2E and does not satisfy migration acceptance. |

No Production Qualification is claimed.

## 10. Acceptance sequence

1. Freeze and merge the ca-agent-next Plugin Package/IPC and Artifact Handoff contract. This PR is aligned to its current proposal.
2. Pass ca-agent-next Core and Generic Echo E2E, including Skill-first selection, both modes through one Runtime Action Boundary, and Plugin Event/Delivery/Tools.
3. Freeze the exact OpenAMSM OpenAPI operationIds, normalized Event fields, NAS discriminator fields, multipart slots, externalRequestId and continuation correlation.
4. Implement the independent Go Plugin against the exact Next package and IPC contract.
5. Integrate ca-client-tools v1.2.0 through the unified Plugin Manager tools adapter; complete the relevant Codec Phase C consumer integration and publish only the exact accepted Codec Tool contracts.
6. Pass fixture/protocol-vector tests for Create, Modify, Release and continuation, including both the no-decode path and a Skill-driven Codec invocation path. Verify the decoded projection never replaces the original artifact.
7. Pass real isolated OpenAMSM Create → Modify → Release E2E with one UE/PSI, raw N1/N2, Owner Receipt/Commit Proof, UNKNOWN/Reconcile and zero duplicate mutation.

The new Plugin is the second independent business used to prove zero Core business coupling. Do not require a separate third Echo-like business after OpenAMSM.
