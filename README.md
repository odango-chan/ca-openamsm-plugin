# CA OpenAMSM Plugin

Independent Go Event Plugin for OpenAMSM SBI protocol ingress and response delivery.

**Normative Host contract:** [ca-agent-next PR #1](https://github.com/odango-chan/ca-agent-next/pull/1). This repository follows its [Plugin Package/IPC v1alpha1](https://github.com/odango-chan/ca-agent-next/blob/docs/architecture-baseline-20261125/docs/plugin-package.md) and [Host Artifact Handoff](https://github.com/odango-chan/ca-agent-next/blob/docs/architecture-baseline-20261125/docs/plugin-artifact-handoff.md). It does not define a separate Plugin API.

The Plugin handles external SBI protocol parsing, normalization and response encoding. Every fresh business Event must bind a Skill in the Host before execution. The Plugin does not select a Skill, execute business logic, call the Owner or implement Tools.

OpenAMSM Owner operations remain in [ca-client-tools](https://github.com/odango-chan/ca-client-tools), consumed through the single Plugin Manager tools extension adapter. Existing Establish, Modify and UE-requested Release Skills remain in [ca-skills](https://github.com/odango-chan/ca-skills).

[Architecture and migration boundary](docs/architecture.md) · [Acceptance gates](docs/acceptance.md)

**Status:** design-only. No Go runtime, SBI listener migration, Host Artifact Handoff implementation or Create/Modify/Release E2E is claimed. The design PR remains OPEN/DRAFT and depends on the Next contract being frozen.
