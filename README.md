# CA OpenAMSM Plugin

Independent Go deployment adapter for OpenAMSM external protocol ingress and delivery.

The Plugin is a long-lived standalone process. It communicates with CA Agent Host through stdio NDJSON and exposes the external HTTP/2 SBI listener. Its package carries the Event Contract manifest and request/response Schemas, which the Host automatically imports.

The Generic LLM Agent is the default event consumer. A Host deployment policy may select an existing CA-SL Skill for deterministic execution. The Plugin does not implement an Agent, Skill Runtime, Tool Provider, Owner client or business authorization mechanism.

[Proposed architecture and acceptance plan](docs/architecture.md)

Related design:
- [CA Agent Host Plugin Contract v0](https://github.com/odango-chan/ca-agent-demo/blob/docs/plugin-architecture-freeze-20261124/docs/design/plugin-contract.md)
- [CA Agent Host Plugin Acceptance](https://github.com/odango-chan/ca-agent-demo/blob/docs/plugin-architecture-freeze-20261124/docs/design/plugin-acceptance.md)
- [Host Plugin design PR #249](https://github.com/odango-chan/ca-agent-demo/pull/249)
- [Plugin implementation tracking #235](https://github.com/odango-chan/ca-agent-demo/issues/235)

Status: design review only. No Go runtime, SBI listener migration, binary artifact handoff or T2/T3 acceptance is implemented or claimed.
