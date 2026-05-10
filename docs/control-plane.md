# mspace Control Plane

> Status: architecture direction and first server skeleton, updated 2026-05-10

## Decision

mspace should split out a real server/control-plane for multiplayer collaboration.

The desktop app and local runner should not own collaboration identity. They should become runtime clients that authenticate to the control plane with mspace-issued tokens.

## Ownership

The control plane owns:

- users;
- workspaces;
- workspace members and roles;
- mspace auth sessions;
- GitHub identity links;
- future GitHub App installation state;
- audit and collaboration sync;
- future runtime registration tokens.

The desktop app owns:

- native shell behavior;
- local UI state;
- local file pickers;
- opening external auth flows;
- local runtime UX.

The runner owns:

- local repo checkout and worktree execution;
- Codex app-server process lifecycle;
- local logs and artifacts while running;
- Kubernetes deploy/test execution until this moves into runtime workers.

## Auth Shape

GitHub is an identity provider, not the product session authority.

```text
Desktop
  -> GET /api/auth/github/start
  -> open GitHub authorizeUrl in the browser
Browser
  -> GitHub OAuth
  -> GET /api/auth/github/callback
mspace server
  -> validate state
  -> exchange code with server-side client secret
  -> user_identities(provider=github)
  -> mspace auth session token
Desktop
  -> poll GET /api/auth/github/result?state=...
  -> store msp_... session token
  -> call mspace APIs with Authorization: Bearer msp_...
```

The server may use a GitHub OAuth client secret because it is a trusted backend environment. The desktop app must not embed GitHub client secrets.

The callback endpoint returns a small success page, not raw auth JSON. The desktop app receives the `msp_...` token through the state-bound result polling endpoint. Poll results are single-use and short-lived.

Future GitHub repository automation should use GitHub App installation tokens stored and rotated by the control plane. Do not build long-lived repository automation on personal GitHub OAuth tokens stored by the desktop or runner.

## First Implementation Slice

The initial `server/` module provides:

- `GET /health`;
- `GET /api/auth/github/start`;
- `GET /api/auth/github/callback`;
- `GET /api/auth/github/result`;
- `GET /api/auth/me`;
- `GET /api/workspaces`;
- `GET /api/workspaces/{workspaceID}/inbox`;
- `POST /api/workspaces/{workspaceID}/issue-events`;
- `POST /api/workspaces/{workspaceID}/issue-events/{eventID}/read`;
- `POST /api/workspaces/{workspaceID}/issues/{issueID}/read-through`;
- Postgres migrations for `users`, `user_identities`, `workspaces`, `workspace_members`, `oauth_states`, `oauth_results`, `auth_sessions`, `issue_events`, `issue_event_receipts`, and `issue_watchers`;
- mspace session tokens with `msp_` prefix;
- a memory-backed store used only by tests.

The desktop now has a lightweight GitHub sign-in entrypoint in the sidebar. Product issue/session data still talks to the local runner for the local MVP, but Inbox read state now has a server-backed team model. After sign-in, the renderer sends the current token and workspace id to the local runner so reviewable issue events can be reported to the control plane. The next integration step is replacing this desktop-provided session handoff with first-class runtime registration tokens.

## Migration Rule

New multiplayer features should land in `server/` first. If a feature involves users, membership, shared issue ownership, GitHub identity, GitHub App installation credentials, audit, or cross-device sync, do not add it only to the local SQLite runner schema.
