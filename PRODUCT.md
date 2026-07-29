# Product

## Register

product

## Users

mspace is for engineering and platform teams that already use coding agents and shared Kubernetes test clusters. Users are developers, tech leads, and platform engineers who need a durable place to create issues, assign agent work, watch progress, review comments, and validate changes against namespace-scoped runtime evidence.

## Product Purpose

mspace is an Inbox, Issue, and Tests workspace for coding agents. It turns a fragmented workflow of agent edits, chat notes, issue trackers, reusable test case lists, reusable test cluster config, and test-cluster validation into one document-style workspace where a project can hold test coverage and an issue can hold the problem statement, discussion, agent session, branch state, selected cluster, issue test namespace, preview URL, Kubernetes evidence, and final review trail.

The product succeeds when a team can create or route an issue, manage project-level test cases, assign a worker-backed agent session, let that agent modify code, then record a branch/PR handoff and run an issue-scoped Kubernetes test deployment or issue-backed test run with enough setup, preview, and evidence to decide what happens next.

## Agent Model

mspace exposes three fixed execution Agents: Codex (`@codex`), Claude Code (`@claude`), and Pi (`@pi`). An Agent is an execution engine, not a stored persona or prompt profile. The product does not offer custom Agent definitions, role instructions, arbitrary command profiles, models, or MCP configuration.

Skills and Workflows are separate. Skills are server-managed, versioned instruction bundles that can be attached to a Session. Workflows are mspace-owned product automations such as issue analysis, triage, Tests, import mapping, deploy, and cleanup; these remain Codex-backed until mspace defines an explicit engine policy for them. Workers are execution hosts, while Environments are the targets those Workers operate.

The Agents route answers two operational questions without turning into a settings marketplace: whether This Mac can run each fixed Agent, and how many connected Workers in the current workspace can claim that Agent's work. A second matrix shows each Worker's liveness and per-engine installation/configuration state; Skills stay in their own tab.

Each Issue has one mutable source line rather than one branch per Agent Session. Human-triggered Codex, Claude Code, and Pi Sessions serialize on one stable Issue branch and continue the same Worker-owned working copy; the Session remains the audit record for inputs, logs, engine references, commits, evidence, failures, and cancellation. Analysis, deploy, Tests, and other server-owned Workflows run outside the mutable Issue working copy, pinned to an explicit source Commit when the Workflow has one, so they cannot move or contaminate the Issue branch. Until Git object transfer exists, the source working copy stays bound to the Worker storage that owns it and the product reports that constraint instead of silently restarting elsewhere.

## Brand Personality

Calm, operational, and exact. The interface should feel like a serious document workspace for real engineering work: quiet enough for daily use, explicit about runtime and namespace scope, and direct about what agents are doing.

The public website can use a sharper brand surface, but it should still be specific about the mspace issue-to-evidence workflow. The product app itself stays calm and document-first.

## Anti-references

- No marketing shell, AI hero page, decorative dashboard, or generic automation pitch inside the product app.
- No terminal-only experience that hides issues, comments, owners, or review state.
- No Sealos API dependency as the primary control path.
- No cluster-wide agent permission model.
- No vague AI vocabulary such as magic, seamless, next-gen, unlock, or elevate.
- No visual treatment that competes with the issue document or runtime evidence.

## Design Principles

- Issues are durable working documents, not transient job cards.
- An Issue owns one stable mutable source branch and current head. Agent Sessions are serialized attempts on that source line, not independent branch identities.
- Inbox is for messages and state changes that need human review, not the full issue database.
- Fixed Agents should appear as collaborators with their own engine identity, Sessions, blockers, and progress updates; Claude Code and Pi must never be presented as Codex.
- Server-managed workflow skills should reduce issue handoff friction for mspace-owned flows and user-invoked issue comments. Workspaces can inspect pinned skill revisions, manage basic custom skills, and enable or disable built-ins, but mspace should not become a generic skill marketplace or remote installer.
- Test cases are durable project objects. Codex can generate or refine Case suggestions, but humans approve suggestions before canonical test coverage changes. Formal test plans can include lightweight setup steps for real preconditions before case execution; setup should stay issue-backed and artifact-driven rather than becoming a separate template product. Case revision history should make changed fields readable from the detail page instead of showing only version titles.
- Runtime evidence belongs next to the issue story: branch, logs, status, selected worker, selected cluster, namespace, preview URL, and environment links should support review.
- Agent execution and Kubernetes validation are separate concepts, and the UI should make that boundary visible when it matters.

## Accessibility & Inclusion

Use a conservative product UI baseline: strong text contrast, keyboard-accessible navigation and dialogs, visible focus states, at least 40px interactive targets where practical, semantic buttons and links, reduced-motion-safe transitions, and status communication that does not rely on color alone.
