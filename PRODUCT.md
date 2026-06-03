# Product

## Register

product

## Users

mspace is for engineering and platform teams that already use coding agents and shared Kubernetes test clusters. Users are developers, tech leads, and platform engineers who need a durable place to create issues, assign agent work, watch progress, review comments, and validate changes against namespace-scoped runtime evidence.

## Product Purpose

mspace is an Inbox, Issue, and Tests workspace for coding agents. It turns a fragmented workflow of agent edits, chat notes, issue trackers, reusable test case lists, reusable test cluster config, and test-cluster validation into one document-style workspace where a project can hold test coverage and an issue can hold the problem statement, discussion, agent session, branch state, selected cluster, issue test namespace, preview URL, Kubernetes evidence, and final review trail.

The product succeeds when a team can create or route an issue, manage project-level test cases, assign a worker-backed agent session, let that agent modify code, then record a branch/PR handoff and run an issue-scoped Kubernetes test deployment or issue-backed test run with enough preview and evidence to decide what happens next.

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
- Inbox is for messages and state changes that need human review, not the full issue database.
- Agents should appear as collaborators with assignees, sessions, blockers, and progress updates.
- Test cases are durable project objects. Codex can generate or refine Case suggestions, but humans approve suggestions before canonical test coverage changes. Case revision history should make changed fields readable from the detail page instead of showing only version titles.
- Runtime evidence belongs next to the issue story: branch, logs, status, selected worker, selected cluster, namespace, preview URL, and environment links should support review.
- Agent execution and Kubernetes validation are separate concepts, and the UI should make that boundary visible when it matters.

## Accessibility & Inclusion

Use a conservative product UI baseline: strong text contrast, keyboard-accessible navigation and dialogs, visible focus states, at least 40px interactive targets where practical, semantic buttons and links, reduced-motion-safe transitions, and status communication that does not rely on color alone.
