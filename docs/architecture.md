# OpenAMSM Go Plugin — Deployment Adapter Design

Status: proposed for review. This repository currently contains design documentation only; no Plugin runtime or protocol migration is claimed.

## 1. Purpose

ca-openamsm-plugin is an independent Go process that adapts OpenAMSM deployment protocols to the generic CA Agent Host Plugin contract. It is not another Agent, Runtime, Tool Provider or Owner client.

The first goal is deliberately narrow: move external Nsmf/Namf protocol ingress and response representation out of the Host while keeping Run lifecycle, execution, business authorization and Owner operations in their existing owners.

## 2. Process and transport

The Host starts one long-lived Go executable using operator-configured argv. The process exchanges UTF-8 NDJSON over stdin/stdout using ca-agent-host.plugin/v0.

The Go process separately exposes the external HTTP/2 SBI listener. h2c is a deployment profile choice for an isolated lab. These are two different links:

- Host ↔ Plugin: stdio NDJSON control/event/delivery protocol.
- AMF / external SBI peer ↔ Plugin: HTTP/2 SBI protocol.

The Go executable does not use Go's in-process plugin/.so loader. It is an ordinary standalone binary. stdout is reserved for IPC; diagnostics go to stderr. The process handles concurrent external requests and serializes IPC writes.

## 3. Plugin-bundled Event Contract

The Plugin package carries its own manifest and local request/response Schemas. The Host loads the manifest automatically when it loads the configured Plugin package and registers the declared event types. There is no separate Event Registry service or per-event Host handler.

Illustrative manifest:

    apiVersion: ca-agent-host.plugin/v0
    plugin:
      id: openamsm
      version: 0.1.0
    ingress:
      - id: openamsm.nsmf
        events:
          - type: nsmf.sm-context.create.v1
            description: Create an SM Context for a PDU Session.
            requestSchema: schemas/create.request.json
            responseSchema: schemas/create.response.json
            artifacts:
              - slot: n1
                mediaType: application/vnd.3gpp.5gnas
              - slot: n2
                mediaType: application/vnd.3gpp.ngap
          - type: nsmf.sm-context.update.v1
            description: Update an existing SM Context.
            requestSchema: schemas/update.request.json
            responseSchema: schemas/update.response.json
          - type: nsmf.sm-context.release.v1
            description: Release an existing SM Context.
            requestSchema: schemas/release.request.json
            responseSchema: schemas/release.response.json
    delivery:
      - id: openamsm.sbi-response

The Host supplies the event description, validated payload, request/response Schemas and relevant artifact references to its Generic LLM Agent. The Agent also discovers available Tools from the Host's registered Provider Interface Packages. The Plugin does not select a model, Tool, Skill or execution mode.

The Generic LLM Agent is the default event consumer. A Host/operator deployment policy may select a CA-SL Skill for a specific event when deterministic execution is preferred. The Event Contract itself describes only event data and response shape; it does not prescribe the consumer.

For this Demo, manifest import is automatic. No package approval, signature, PKI, sandbox or Plugin marketplace is required. Basic manifest and Schema validation remains for correctness.

## 4. End-to-end responsibility

    AMF / external SBI peer
       │ HTTP/2 SBI
       ▼
    Go ca-openamsm-plugin
       - SBI listener and protocol handling
       - external peer authentication as required by the selected lab profile
       - Nsmf parsing and multipart / Content-ID association
       - NAS/N2 protocol decoding and Event normalization
       - Plugin-bundled Event Contract
       - SBI response encoding and delivery
       │ stdio NDJSON: ca-agent-host.plugin/v0
       ▼
    Generic CA Agent Host
       - automatic Event Contract import and payload validation
       - event identity, RunStore and response obligations
       - default Generic LLM Agent consumption
       - optional Host-selected CA-SL Skill
       - Host Tool/Provider boundary and authorization
       - response Schema and business outcome validation
       - Host resource/alias map and public resource URI generation
       - HostArtifactStore and delivery records
       ▼
    Existing ca-client-tools Provider
       - published Tool Interface Package
       - Tool implementation and private backend mapping
       - Owner calls, Receipt / Commit Proof / Reconcile
       ▼
    OpenAMSM Owner / SMF
       - authoritative SM Context identity, existence and lifecycle

The Host owns resource/alias mapping and supplies the public resource URI. The Plugin renders that URI into the SBI Location header and owns wire-level response serialization. The Plugin converts external protocol messages into events and structured protocol responses; it does not maintain a second resource database or decide how the Agent performs the work.

## 5. Generic Agent and optional Skill path

The default path is:

    SBI request
      → Plugin normalized event
      → Host auto-imports Event Contract and validates payload
      → Generic LLM Agent receives description, payload, Schemas and available Tools
      → Agent dynamically requests Tool calls through Host
      → Host authorizes and invokes the existing Provider
      → Agent produces a structured response
      → Host validates response Schema and business evidence
      → Plugin encodes the SBI response

No OpenAMSM-specific LLM handler or pre-authored Skill is required for this default path.

For an event configured to use a deterministic Skill, the Host selects that Skill and invokes the existing Admission/READY/CA-SL Runtime path. The Plugin and Event Contract do not choose the Skill. There remains one CA-SL Runtime and one Provider system.

The Agent cannot connect directly to the OpenAMSM Owner or establish COMMITTED by generating text. For mutations, the existing Provider/Owner Receipt and Commit Proof semantics remain authoritative; UNKNOWN must remain UNKNOWN.

## 6. Event identity and response obligation

Event identity is the identity of one logical business event. It must be derived from a stable protocol retry identifier or a documented immutable request identity, not an HTTP/2 stream ID or IPC frame ID.

The Host compares the event identity and semantic content digest separately from the response obligation:

- Same event identity and same semantic digest: reuse the original Run.
- Same event identity and same semantic digest but a new HTTP/2 stream: attach a new response obligation to that same Run and deliver the same stored/current result to the new waiter.
- Same event identity with different semantic content: conflict; do not execute again or replace the original event.

The Plugin maintains the current protocol waiter mapping. A Plugin restart may lose a live HTTP/2 stream; a protocol retry creates a new obligation and reuses the Host Run result. The Host persists event identity, content digest, Run association and obligations in the existing RunStore. No Plugin database is introduced.

Runtime continuation wakeups and external protocol delivery are distinct Run records. The Host records the structured result and delivery ID before dispatch. The Demo uses one delivery attempt per obligation; an ambiguous result is UNKNOWN. A delivery result never changes the Owner business outcome.

The precise stable retry identity and waiter recovery rules must be verified for Create, Modify and Release during T3. Where the protocol has no trustworthy retry identity, document and test the selected derivation before migration.

## 7. T3 Host-managed N1/N2 artifact handoff

N1 NAS, N2 NGAP and multipart binary parts must not be Base64-embedded in Event JSON or stored in a Plugin-owned database.

The Plugin declares typed artifact slots and associates the received raw bytes with protocol Content-ID. The Host owns the handoff and custody:

1. Host creates a temporary per-Plugin staging directory and supplies it in host.initialize when T3 artifact transfer is enabled.
2. Plugin writes raw N1/N2 bytes under that directory and sends only a relative filename plus slot, media type, Content-ID, byte length and SHA-256 in the NDJSON event.
3. Host reads and verifies the actual bytes, imports immutable content into the existing HostArtifactStore, and attaches the reference to the Run in RunStore before ACK or execution.
4. For outbound data, Host writes only the authorized response artifact into the staging directory and dispatches its descriptor; Plugin reads the raw bytes and encodes the SBI response.
5. Host cleans temporary files after import/delivery and on restart.

This simple local file handoff keeps binary bytes out of NDJSON and avoids Base64, a new network service or a Plugin database. HostArtifactStore and RunStore remain authoritative. T2 is JSON-only and does not receive an artifact staging directory.

## 8. Codec reuse

Reuse the shared Go NAS / N2 / NGAP codec package from ca-client-tools when its public API and required message coverage are available. Do not copy codec implementations or launch a second codec worker from this Plugin.

Keep protocol codec, SBI multipart parsing, Event mapping and response formatting as separate responsibilities. Compare codec behavior against sanitized cross-language golden vectors and malformed-input cases. Claim only the exact tested message/IE subset; do not infer full NGAP parity from a narrower NAS/N2 result.

## 9. Migration boundary

| Existing ca-agent-demo responsibility | Destination |
| --- | --- |
| Nsmf HTTP/2 listener, HTTP wire parsing, multipart/Content-ID, protocol decoding and response encoding | Go Plugin |
| External protocol normalization and Event Contract | Go Plugin |
| NAS/NGAP/N2 codec adapter | Go Plugin using shared Go codec |
| Run identity, event replay, response obligations and waiter-to-Run association | Generic Host / RunStore |
| Nsmf Create/Modify/Release Run lifecycle and NsmfRunExecutor | Generic Host / Resident |
| Resource map, Host Location/Run correlation, Owner claims and release confirmation | Generic Host / Resident |
| HostArtifactStore and private artifact access | Generic Host |
| MAS Callable, ToolCatalog, Owner client, Receipt, Commit Proof and Reconcile | Existing ca-client-tools Provider + Host/Runtime |
| Skill Procedure and business Goal | ca-skills |
| Single CA-SL Procedure executor | CA-SL Runtime |

Do not move NsmfRunExecutor, Resident lifecycle, RunStore, HostArtifactStore, resource mapping, Owner claims, MAS Callable mapping, ToolCatalog, Owner client, Receipt/Commit/Reconcile or CA-SL Runtime into this repository.

Do not move the entire integrations/fivegc directory. nsmf_http.py and nsmf_runtime.py contain mixed protocol and Host composition responsibilities and must be split by ownership. The Plugin is not a second Provider and must not become a direct Owner client.

## 10. Acceptance sequence

| Stage | Required evidence |
| --- | --- |
| Design | Align this repository with Host Plugin Contract v0, automatic Event Contract import, Generic Agent default and optional Skill override |
| T1 | Host Tool P0 #241 accepted; no Provider work duplicated here |
| T2 | Non-5GC Go Plugin → automatic manifest import → Generic LLM Agent → dynamic existing Provider Tool call → structured response → Plugin delivery |
| T3 contract | After T2 and relevant Host/Provider gates, freeze Nsmf retry identity, waiter mapping and Host-managed artifact transfer |
| T3 regression | Isolated Scheme C Create/Modify/Release through normal Host startup, Agent/optional Skill, Provider and Owner; verify N1/N2 bytes, Location, receipts, UNKNOWN/reconcile and no duplicate mutation before legacy Listener removal |

Current status: design only. T1 remains with #241. PR #246 synthetic Owner G5 reports 22 passed; real isolated Scheme C G6 remains NOT_RUN / BLOCKED and G7 remains partial. T2 and T3 are NOT_STARTED. No Production Qualification is claimed.
