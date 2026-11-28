# OpenAMSM Go Plugin — Deployment Adapter Design

**Status:** design-only proposal aligned to ca-agent-next. No Go runtime, SBI listener migration, Artifact Handoff implementation or E2E acceptance is claimed.

**Current central design candidate:** `ca-agent-next` `integration/issue-50-mainline-convergence` (PR #115; not yet merged to `main`), snapshot `f8de225054895f53b30f73aa721bd1ca9290813e`.
- [Plugin Package / IPC v1alpha1](https://github.com/odango-chan/ca-agent-next/blob/f8de225054895f53b30f73aa721bd1ca9290813e/docs/%E8%A7%84%E8%8C%83/%E6%8F%92%E4%BB%B6%E5%8C%85%E4%B8%8E%E8%BF%9B%E7%A8%8B%E9%80%9A%E4%BF%A1%E5%8D%8F%E8%AE%AE%E8%A7%84%E8%8C%83.md)
- [F05 Context / Artifact staging](https://github.com/odango-chan/ca-agent-next/blob/f8de225054895f53b30f73aa721bd1ca9290813e/docs/%E8%AE%BE%E8%AE%A1%E8%AF%B4%E6%98%8E%E4%B9%A6/F05-Context%E4%B8%8EArtifact%E7%AE%A1%E7%90%86/Context%E4%B8%8EArtifact%E7%AE%A1%E7%90%86%E8%AE%BE%E8%AE%A1%E8%AF%B4%E6%98%8E%E4%B9%A6.md)
- [F06 OpenAMSM Event contract](https://github.com/odango-chan/ca-agent-next/blob/f8de225054895f53b30f73aa721bd1ca9290813e/docs/%E8%AE%BE%E8%AE%A1%E8%AF%B4%E6%98%8E%E4%B9%A6/F06-%E4%B8%9A%E5%8A%A1%E6%8E%A5%E5%85%A5%E4%B8%8E%E9%9B%86%E6%88%90/%E4%B8%9A%E5%8A%A1%E6%8E%A5%E5%85%A5%E4%B8%8EOpenAMSM%E9%9B%86%E6%88%90%E8%AE%BE%E8%AE%A1%E8%AF%B4%E6%98%8E%E4%B9%A6.md)
- [SR-009 module requirements](https://github.com/odango-chan/ca-agent-next/blob/f8de225054895f53b30f73aa721bd1ca9290813e/docs/SR/SR-009-OpenAMSM%20SBI%20%E4%BA%8B%E4%BB%B6%E6%8F%92%E4%BB%B6%E4%B8%8E%E4%BA%8C%E8%BF%9B%E5%88%B6%E4%BA%A7%E7%89%A9%E4%BA%A4%E6%8E%A5.md)

The Plugin Package / IPC v1alpha1 contract is already merged in Next. PR #115 carries the central design integration candidate and remains Draft. The SHA above is an **immutable snapshot** for comparison, not a self-updating current Head or a main merge claim; its F05 v0.4.13 / F06 v0.3.9 / SR-009 v0.2.15 / IPC v0.2.8 revisions remain design / review scoped. Re-read the current PR #115 Head and preserve per-AC NOT_RUN / BLOCKED boundaries before implementation or Ready.

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
            - slot: n2Ext1.request
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
            - slot: n2.request
              mediaType: application/vnd.3gpp.ngap
              required: false
            - slot: n2Ext1.request
              mediaType: application/vnd.3gpp.ngap
              required: false
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

The Event names above are normalized internal Event types. **Next F06 v0.3.9 §4.1 already fixes their operationId, request root, discriminator and Content-ID/slot mappings**; implementation must use those rules and test their compatibility against the pinned original TS 29.502 OpenAPI without reopening or guessing the mapping. The manifest is illustrative: the `required` attribute must not elevate an OAS3-optional part to universally required; a stricter Demo ingress profile must be explicit and independently justified. The Plugin does not add a Skill ID, Goal, business Contract Package, execution mode or authorization grant.

If the deployment includes AMF/network-initiated ReleaseSMContext, it is a distinct protocol event and must not be conflated with the UE-requested PDU Session Release Skill. Add its normalized event only when that scope is explicitly included.

## 3. Skill-first event consumption

Every fresh business Event must bind a Skill before execution. There is no unbound Generic LLM Agent fallback.

The Plugin publishes only:
- declared normalized `eventType`;
- the validated normalized payload defined by F06 §4.1;
- protocol source and optional `externalRequestId` only when the trusted peer supplies a stable request identity;
- raw Artifact staging descriptors. Host `artifactRef` values remain Host-internal and are never sent to this Plugin.

It does not select or name a Skill. It does not decode N1/N2 for business reasoning. For shared `UpdateSmContext`, it consumes only the narrow T0 5GSM discriminator API published by `ca-client-tools` to normalize `0xC9` / `0xD1` / `0xD4`; it must not implement or copy a NAS parser, invoke full `DecodeNAS` as a shortcut, or expose business IE fields before Skill selection. Until the exact shared API and version are published, the Plugin must fail closed for production UpdateSMContext classification; a test double may test Event mapping only. The Host Resolver selects the Skill. Deeper protocol fields remain available through on-demand Codec Tools.

### 3.1 On-demand protocol decoding

- If a Skill does not need inner NAS/NGAP fields, it passes the original Host-authorized `artifactRef` to the relevant business Tool. No Codec Tool is invoked.
- If a Skill needs protocol fields, it calls the corresponding Codec Tool exported by the `ca-client-tools` Tool Plugin through the common Runtime Action Boundary, after Skill/Run binding.
- The Codec Tool returns a structured JSON projection. The original N1/N2 artifact remains unchanged and available for forwarding or response composition.
- Encode, when needed, is also a Tool operation and returns an output artifact for Host-controlled delivery.
- Codec operations must be published in the canonical Tool Interface Package and limited to the exact message subset that has been implemented and tested.

The Host Skill Resolver and CA-SL authoring contract are CA-SL **1.6.0 only**. This Plugin does not implement or duplicate selection semantics:

- `when` is declared only in `assurance/contract.yaml`; it is not a `SKILL.md` front-matter field.
- The Host Resolver owns continuation lookup, deterministic `when` evaluation, candidate selection and Skill binding.
- No v1alpha5 compatibility, aliases, fallback parser, mixed-version negotiation or automatic migration is supported.
- Execution mode is chosen only after exact Skill binding. No Assurance means LLM-guided execution; valid Assurance supports Deterministic and LLM-guided execution through the same Runtime Action Boundary; invalid present Assurance is an import failure, not a silent downgrade.

The existing ca-skills packages are the business source:

- establish-pdu-session-mas;
- modify-pdu-session-mas;
- release-pdu-session-mas.

Create, Modify and UE-requested Release must be distinguished by the normalized Event metadata and validated request fields defined in F06 §4.1. Modify and UE-requested Release may share an Nsmf UpdateSMContext ingress; the Resolver, not the Plugin, chooses the Skill. Do not infer the Skill from an HTTP path alone or duplicate business selection logic in Go.

## 4. Event identity, Run and response obligations

The Plugin distinguishes optional logical request identity from the current HTTP/2 response stream:

- `externalRequestId` is optional. In the controlled Demo peer it is populated only from the configured private `X-CA-Request-Id` header; it is not a 3GPP OpenAPI field. If a real peer supplies no stable identity, omit it. Do not synthesize a canonical ID from `smContextRef`, Content-ID, principal, HTTP/2 stream ID or IPC `messageId`.
- `responseChannelId` is a Plugin-local handle for the current HTTP/2 stream.
- `eventId` identifies the normalized Event instance.
- Host owns Run ID, deduplication, continuation association and `responseObligationId`. `ingress.ack` returns only `eventId`, disposition and safe diagnostics; it never returns Run ID, `responseObligationId` or Host `artifactRef`.

Never use HTTP/2 stream ID or IPC `messageId` as logical business request identity.

For an identical retry with a trusted stable `externalRequestId`, the Host may attach a new response obligation to the original Run. Reuse with different normalized content or artifact bytes is a Host-side conflict. The Plugin does not deduplicate or associate Runs; it retains only the live stream waiter required to write the current response. `responseObligationId` is received only transiently in `delivery.dispatch` and is never persisted, forwarded or returned in `delivery.result`.

If a stable retry identity cannot be established, cross-reconnect replay-safe E2E remains NOT_RUN/BLOCKED. Do not add an Event content digest or extra SHA-256 mechanism.

A delivery result reports delivered, failed or unknown. An ambiguous delivery remains UNKNOWN and does not change the Owner's business outcome. Create 201 and Location are serialized from Host-supplied status/headers; Plugin does not invent the resource URI or infer business commitment.

## 5. Host-owned N1/N2 Artifact Handoff

Binary data uses the exact ca-agent-next Plugin Package / IPC v1alpha1 and F05 staging contract.

The Host creates inbound/outbound staging directories and passes them in `host.initialize`. The Plugin writes raw inbound N1/N2 parts under the supplied inbound root and sends relative `stagingPath`, `slot`, `mediaType`, Content-ID when present and `byteLength` in `ingress.event`. Host validates the file and imports it into its ArtifactStore; any `artifactRef` remains Host-internal. `ingress.ack` returns only `eventId`, `disposition` and safe diagnostics, never `artifactRef`, Run ID or `responseObligationId`.

For outbound delivery, Host stages only authorized N1/N2 bytes for the exact response obligation and sends `delivery.dispatch` with `deliveryId`, `responseObligationId`, `responseChannelId` and outbound descriptors. Plugin uses only the current `responseChannelId` waiter, constructs the correct SBI multipart/Content-ID response and returns `delivery.result` with `deliveryId` and disposition; it does not echo Host identities.

### 5.1 OpenAMSM slot vocabulary

Use the shared slot vocabulary defined by Next:

| Slot | Direction | Meaning |
|---|---|---|
| n1.request | Inbound | Original 5GSM NAS request |
| n2.request | Inbound | N2 SM information when the Create/Update JSON actually references it |
| n2Ext1.request | Inbound | Create/Update `n2SmInfoExt1.contentId` reference to the original N2 Ext1 part, independent of `n2.request` |
| n1.confirmation | Inbound | N1 continuation, including PDU Session Release Complete (0xD4), when delivered as a later ingress |
| n2.confirmation | Inbound | N2 continuation, including PDU Session Resource Release Response, when delivered as a later ingress |
| n1.response | Outbound | Owner/Host-produced N1 response for the recorded obligation |
| n2.response | Outbound | Owner/Host-produced N2 response for the recorded obligation |

NAS media type: application/vnd.3gpp.5gnas. NGAP/N2 media type: application/vnd.3gpp.ngap.

Establish uses the original N1 bytes when present under its published OAS3/Demo ingress profile; optional `n2.request` and `n2Ext1.request` are staged **only when the Create/Update request JSON references their Content-ID**. Update Modify carries 0xC9; UE-requested Release carries 0xD1. Release continuation may carry N1 Release Complete 0xD4 and N2 PDU Session Resource Release Response; Host correlates both to the original Run/SUPI/PSI/generation. **No `n2Ext1.confirmation` slot has been frozen**: any continuation with N2 Ext1 is explicitly rejected in this profile and its operational sub-acceptance remains NOT_RUN; never drop/rename the bytes silently.

Implement exactly the operationId/request/Content-ID/slot mappings already specified in Next F06 §4.1, validated by test fixtures from the fixed original TS 29.502 source. Unknown, duplicate, missing, mismatched media type or extra unreferenced parts must fail closed according to the published ingress profile; do not replace the frozen event contract with a separate Plugin-only mapping or modify the upstream 3GPP YAML.

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

The shared Go codec has one implementation owner: `ca-client-tools`. Two separate surfaces must not be conflated.

### 7.1 T0 pre-selection classifier

- The Event Plugin needs only the narrow 5GSM discriminator to map shared `UpdateSmContext` ingress to the correct normalized Event before Skill selection.
- This is a library-level API owned by `ca-client-tools`, not an Agent-callable Tool, Capability, Provider binding, second Plugin or worker process.
- It validates the F06 minimum header contract and returns only `0xC9`, `0xD1`, `0xD4`, or an explicit malformed/unsupported outcome. It does not parse IEs or expose business fields.
- The exact exported Go package, API signature and version must be frozen and published by `ca-client-tools`. This consumer must not guess a function name/module version, copy a parser, call full `DecodeNAS` as a shortcut or create a second worker.
- Until that API is published and pinned, production UpdateSMContext classification is blocked. Test doubles may verify Event mapping logic only.

### 7.2 T2 on-demand Codec Tool

- Full NAS/NGAP/N2 Decode/Encode operations are ordinary Tool Callables owned by the `ca-client-tools` Tool Plugin and published through its canonical Tool Interface Package and existing `tools` extension.
- A selected Skill invokes a Codec Tool only after Skill/Run binding and only when it needs protocol fields, through the common Runtime Action Boundary. If it does not, the original artifact reference is passed to the business Tool unchanged.
- Codec Result Schema/Profile and qualification are independent of the T0 discriminator. The original N1/N2 artifact remains unchanged and available for forwarding or response composition.
- The Event Plugin remains responsible for SBI framing, multipart parsing, Content-ID association and raw byte handoff. It does not call a second codec worker or duplicate codec logic.

Current ca-client-tools PR #39 evidence: OPEN / non-Draft; Phase B FINAL_PASS / PHASE_C_READY; Phase C consumer migration NOT_STARTED. The Phase B result does not publish the T0 classifier API or the T2 Agent-callable Codec Tool contract.

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

## 9. Dependency and evidence status — 2026-11-28

| Dependency | Current verified state | Consequence |
|---|---|---|
| ca-agent-next Plugin Package/IPC v1alpha1 | Merged normative contract; current F05/F06/SR-009 design candidate is in PR #115 `integration/issue-50-mainline-convergence` (Draft, not main) | Build only against the pinned current candidate and the merged IPC contract; do not use obsolete `docs/architecture-baseline-20261125` paths. |
| ca-agent-next PR #115 | OPEN / Draft; mainline integration candidate | The central SR/F05/F06 snapshot is not yet merged to main; pin the reviewed SHA in every implementation report. |
| ca-client-tools PR #39 | OPEN / non-Draft; Phase B FINAL_PASS / PHASE_C_READY; Phase C consumer migration NOT_STARTED | Does not by itself publish the T0 classifier API or the T2 Agent-callable Codec Tool contract. |
| ca-client-tools Issue #44 | OPEN; request to publish the narrow T0 5GSM classifier API | Production AC-009-04 remains blocked until the owner freezes and publishes the exact Go API and version. |
| ca-client-tools v1.2.0 | Provider v2 / Interface Package implementation | Consume only through the single Next Plugin Manager adapter; no second Provider Manager or direct Owner client. |
| ca-openamsm-plugin PR #1 | OPEN / Draft; design documentation only | No Go runtime, SBI listener migration, Artifact Handoff implementation or module acceptance is claimed. |
| Next Python Host / ArtifactStore / Channel | NOT_IMPLEMENTED for the required OpenAMSM path | Plugin module tests use a test Host; real Host integration remains a later system gate. |
| OpenAMSM real Create/Modify/UE Release E2E | NOT_RUN | Must be tracked by SR-011 after the required Host/Owner/Provider/Skill artifacts are ready. |

No Production Qualification is claimed.

## 10. Acceptance sequence

1. Use the merged Plugin Package/IPC v1alpha1 contract and the pinned Next F05/F06/SR-009 candidate; no legacy IPC or compatibility path.
2. After the current SR-009 incremental review approves the scoped module design, implement the Go Event/Delivery Plugin independently with a test Host and protocol fixtures. This module work does not wait for Echo, Next Python Host, Owner or Provider system integration.
3. Implement Create JSON/multipart, Content-ID/slot mapping, raw N1/N2 staging, three normalized Event declarations, HTTP/2 responseChannel waiters and Delivery serialization.
4. For UpdateSMContext, keep the production classifier adapter disabled/fail-closed until ca-client-tools#44 publishes its exact T0 classifier API; test doubles may cover Event mapping but cannot claim production AC-009-04 PASS. Do not copy a NAS parser or call full DecodeNAS.
5. Keep T2 full Codec Tool consumption separate: it is invoked only by a selected Skill through the Runtime Action Boundary after its canonical Interface Package/Provider contract is published.
6. Run the SR-009 module tests with the test Host/peer and report each AC separately. Host Run/dedup/responseObligation, Next ArtifactStore/Channel, Owner Receipt/Proof/Reconcile and real NF E2E remain outside this module Goal and are tracked by SR-010/SR-011.
7. Run real isolated OpenAMSM Create → Modify → UE-requested Release only under SR-011 after the required Host/Owner/Provider/Skill artifacts are available; do not claim Production Qualification.

The new Plugin is the second independent business used to prove zero Core business coupling. Do not require a separate third Echo-like business after OpenAMSM.

**2026-11-28 review correction (ca-agent-next #116 P2-01/P2-07/P2-08):** this is the design-only source PR. Existing stacked implementation PR #3 has separate module test-Host evidence and is not merged; module acceptance does not wait for SR-011 system E2E or SR-026 T2 Codec publication. Fixed central permalink is a historical design snapshot; the latest PR #115 must be checked again before integration.
