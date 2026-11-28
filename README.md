# CA OpenAMSM Plugin

Independent Go Event Plugin for OpenAMSM SBI protocol ingress and response delivery.

**Normative Host contract:** ca-agent-next Plugin Package v1alpha1 / Plugin IPC v1alpha1 and F05 staging. Current central design candidate: [PR #115 integration snapshot](https://github.com/odango-chan/ca-agent-next/blob/39e0bb018e8cd1cc282da3853636c5666dd9a86b/docs/SR/SR-009-OpenAMSM%20SBI%20%E4%BA%8B%E4%BB%B6%E6%8F%92%E4%BB%B6%E4%B8%8E%E4%BA%8C%E8%BF%9B%E5%88%B6%E4%BA%A7%E7%89%A9%E4%BA%A4%E6%8E%A5.md). PR #115 is Draft and not yet merged to `main`; the Plugin Package / IPC contract itself is already merged in Next.

The Plugin handles external SBI HTTP/2 parsing and response encoding, JSON and `multipart/related` parsing, Content-ID association, raw N1/N2 staging descriptors and normalized Event publication. It preserves original N1/N2 bytes through the Host-owned Artifact Handoff. It does not own ArtifactStore or `artifactRef`, and `ingress.ack` does not expose Run identity or Host artifact references.

For shared `UpdateSmContext`, the Plugin consumes only the narrow T0 5GSM discriminator API owned by `ca-client-tools` to distinguish Modify (`0xC9`), UE-requested Release (`0xD1`) and Release continuation (`0xD4`). It must not copy a NAS parser, invoke full `DecodeNAS` as a shortcut, or select a Skill. The exact shared API is pending [ca-client-tools#44](https://github.com/odango-chan/ca-client-tools/issues/44); production UpdateSMContext classification remains blocked until that API is merged/published and pinned. Test doubles can validate Event mapping only.

Every fresh business Event binds a Skill in the Host before execution. CA-SL 1.6.0 is the only target; `when` is declared only in `assurance/contract.yaml`. The Host Resolver owns Skill selection and continuation. The Plugin does not select a Skill, execute business logic, call the Owner or implement Tools.

OpenAMSM Owner operations and full on-demand Codec Tools remain in [ca-client-tools](https://github.com/odango-chan/ca-client-tools), consumed through the single Next Plugin Manager `tools` extension adapter. A selected Skill invokes a published Codec Tool only after Skill/Run binding through the common Runtime Action Boundary; the original N1/N2 artifact remains unchanged.

[Architecture and migration boundary](docs/architecture.md) · [Acceptance gates](docs/acceptance.md)

**Status:** design-only. PR #1 remains OPEN/Draft. No Go runtime, SBI listener migration, Host ArtifactStore implementation, production classifier binding, module acceptance or Create/Modify/Release E2E is claimed.