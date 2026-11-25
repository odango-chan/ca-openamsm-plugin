# OpenAMSM deployment adapter design

Status: proposed for review. This repository contains design documentation.

## Process and ownership

The Host starts one long-lived Go executable using operator-configured argv and exchanges UTF-8 NDJSON over stdio using the Host Plugin v0 contract. The executable exposes its own HTTP/2 SBI listener. h2c is a profile choice for an isolated deployment. The process reserves stdout for IPC, serializes writes, logs sanitized diagnostics on stderr, bounds frames, queued work and request lifetimes, and drains within the Host shutdown deadline. No in-process Go dynamic loading is required.

| Component | Authority |
| --- | --- |
| Go Plugin | Transport and AMF peer authentication, Nsmf wire parsing, multipart/Content-ID association, protocol-level NAS/N2 decode, normalized event Schema and mapping, SBI resource representation and alias resolution where needed, response encoding and delivery. |
| Host | Operator configuration, event Schema validation, Root routing and projection, Admission/READY, durable Run identity and replay, scoped opaque Run-to-resource correlation, private artifact custody, delivery records and optional built-in orchestration. |
| CA-SL Runtime | Executes the admitted Procedure once per accepted invocation path and enforces Root-scoped Capability/Operation authorization and Effect semantics. |
| Tool Provider and Owner | The published Interface Package defines public tools, Schemas, Capabilities and Effects. The Provider performs bound Owner calls via its own connection. The Owner/SMF is authoritative for SM Context identity, existence and Create/Modify/Release transitions. The Host/Runtime verifies evidence and preserves UNKNOWN, using exact-original reconcile where authorized. |

A protocol ACK or successful SBI delivery cannot establish a committed mutation. The Plugin receives the authorized response data needed to render the wire reply. The Generic Host may persist a scoped opaque resource reference for Run correlation and authorized private material access. The OpenAMSM Plugin owns SBI representation, including public `Location`, alias resolution where needed, and protocol status mapping. The OpenAMSM Owner/SMF owns authoritative SM Context identity, existence and Create/Modify/Release transitions. Plugin mappings and Host correlation records carry no authority over business state.

An identical Release replay resolves to its original Run without another dispatch. A distinct request for a released context follows the authorized Owner path. The Plugin maps the Owner's result to the SBI response, preserving ambiguity when the business outcome is UNKNOWN.

## Stable event identity and response obligation

A logical event has a stable adapter event ID under the configured Plugin, ingress and authenticated source. Derive it from the profile's trustworthy retry identifier or documented immutable request identity. IPC frame IDs, connections and transient waiters are excluded. The Host compares canonical semantic content, including typed artifact slot/digest/length when applicable. An identical replay reuses the original Run. Reuse of a key with different semantic content is a conflict.

The response obligation is separately recorded against the original Run and same-Plugin delivery extension. The Plugin may retain or reattach a local waiter only for the same authenticated logical request and same opaque obligation. A changed or absent obligation on a response-bearing replay cannot replace the original or trigger another delivery. The precise Nsmf identity and waiter recovery mapping must be proven for Create, Modify and Release. The Demo records one attempt. Ambiguous delivery is UNKNOWN, independently of business outcome.

## T3 Host-managed artifact handoff

Before acknowledging a new event or executing it, the Host must acquire custody of required raw N1/N2 as immutable bytes, verify bounds and integrity itself, and durably bind typed references to the authenticated ingress and accepted Run. The Plugin supplies only normalized business data to the event. Host-private references and bytes are excluded from ordinary Skill input, LLM context and diagnostics. Authorized private Runtime/Provider material is resolved from the Run and operation scope. For outbound binary content the Host grants the original delivery binding access only to the exact response artifacts for that Run. The Plugin encodes the response without choosing or modifying business outputs.

T3 requires a separately reviewed transfer binding. Candidate reuse is the Host's artifact storage and Run ledger, with durable staging followed by Run attachment, cleanup of abandoned data, and access tied to the bound Run and delivery attempt. Payload transport, authorization of staging and retrieval, limits, retention and expiry still require selection. T2 uses only the baseline JSON contract.

## Codec reuse and migration

Evaluate maintained Go NAS and NGAP/N2 codecs for the chosen 3GPP profile, pinned versions, compatible licenses, relevant message/IE coverage and malformed-input handling. Keep codec adapters pure where possible and compare behavior against sanitized cross-language golden vectors. Keep SBI multipart handling, event projection and response formatting outside reusable codecs. A protocol decode mapping does not replace an already-authorized Provider READ used by a Procedure. The first extraction covers the listener, wire parsing, normalization, serialization and any required SBI alias mapping. Treat existing Host-side Nsmf lifecycle behavior as regression evidence while moving lifecycle authority to the Owner boundary. Retain the existing Provider execution path.

## Acceptance sequence

| Stage | Required evidence |
| --- | --- |
| Design | Review the responsibilities, event replay rules and artifact custody and identify unresolved T3 contract choices. |
| T1 | Host Tool cutover acceptance, including independent Provider/Skill proof, is tracked by the Host project. |
| T2 | An independent non-5GC adapter exercises the built Host wheel, normal Admission/READY, existing Runtime/Provider and paired delivery using deployment configuration only. Cover replay, conflicting content or obligation, invalid payload and process failure. |
| T3 contract | After T2 and relevant Tool/Owner acceptance, approve event/retry identity, artifact binding and response recovery. Validate codec parity and sanitized fixtures. |
| T3 regression | In the isolated Scheme C profile verify Create/Modify/Release through normal startup, Plugin-rendered Location and Owner-authoritative resource lifecycle, exact correlation, N1/N2 bytes, receipt/commit proof, UNKNOWN/reconcile, and no duplicate effect or delivery. Compare against the accepted baseline before removing legacy wiring. |

T3 remains pending. The detailed Host integration evidence and acceptance tracking are maintained in the Host project.
