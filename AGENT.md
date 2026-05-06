# AGENT.md

This project is the product planning and future implementation workspace for mspace.

## Product Direction

mspace is an Inbox and Issue workspace for coding agents. It lets a team manage work as document-style issues, run development sessions locally in the current phase, and deploy or validate those changes in a real namespace-scoped Kubernetes environment.

The product should stay narrow:

- interaction inspiration: Multica-style inbox, issue, and teammate workflow;
- technical inspiration: Optio-style Kubernetes-hosted agent runtime;
- core difference: document-first issue collaboration with attachable real test environments for coding agents.

## Working Rules

- Keep Inbox and Issue objects as first-class product objects.
- Keep local development runtime as the MVP default.
- Keep Kubernetes as the default deployment and test environment.
- Do not rely on Sealos UI APIs as the primary control path.
- Prefer namespace-scoped operations and explicit RBAC.
- Treat `kubectl` as acceptable for prototypes, but prefer structured Kubernetes APIs for durable product logic.
- Do not design cluster-wide agent permissions.
- Do not let agents read Secrets by default.
- Every write-capable session must have an audit trail and a cleanup path.
- Do not fork Multica; use it as a structural reference only.
- Keep docs current when product decisions change.

## Documentation Map

- `docs/product.md`: inbox and issue product positioning, users, workflows, MVP, non-goals.
- `docs/architecture.md`: collaboration layer, runtime layer, permission model, data sketch, risks.
- `docs/ia.md`: MVP navigation, screen map, page regions, state model, build sequence.
- `docs/references.md`: notes from Multica and Optio references.

## Current Non-Goals

- Generic AI agent platform.
- Generic DevOps troubleshooting chatbot.
- Agent skill/rule management product.
- Automatic merge pipeline.
- Cluster-wide Kubernetes assistant.
- Sealos API wrapper.
- Direct Multica code inheritance as the product baseline.

## Preferred Vocabulary

Use these terms consistently:

- Workspace: the team boundary for members, issues, agents, and runtime policy.
- Inbox Item: triage unit for incoming work.
- Project: a repository plus runtime policy.
- Issue: the durable collaboration document for one unit of work.
- Agent Session: one agent run attached to one issue.
- Validation Environment: where the changed project is deployed and tested.
- Project Namespace: a long-lived namespace owned by one project.
- Session Namespace: a temporary namespace owned by one agent session.
- Runtime Provider: the mechanism that starts the agent workspace.
- Scoped Kubeconfig: kubeconfig bound to a session ServiceAccount and namespace policy.

## Product Taste

The UI should feel operational and work-focused. Avoid marketing-heavy pages, decorative dashboards, and abstract AI terminology. The first screen should help a developer answer:

- Which issues need attention now?
- Which agent sessions are attached to each issue?
- What is the agent doing now?
- Which runtime and namespace is it operating?
- Where is the environment?
- What branch or PR did it produce?
