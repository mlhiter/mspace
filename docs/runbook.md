# mspace Local Runbook

> Status: local MVP operations guide, updated 2026-05-07

## Local Data

| Path | Purpose |
| --- | --- |
| `~/.mspace/mspace.db` | SQLite database used by the Go runner. |
| `~/.mspace/repos/<owner>/<repo>` | Cached clone path for GitHub-imported repositories. |
| `~/.mspace/workdirs/<project-id>/<session-id>` | Git worktree created for one local agent session. |
| `~/.mspace/workdirs/_contexts/<session-id>.md` | Markdown session context written before the agent command starts. |

The session worktree path is also stored in `agent_sessions.workdir`.

## Start The App

Install dependencies:

```bash
pnpm install
```

Start desktop:

```bash
pnpm dev:desktop
```

Electron starts the local Go runner automatically if `GET /health` is not already healthy on the configured port.

Run the runner separately for API debugging:

```bash
pnpm runner
pnpm dev:desktop
```

## Environment Variables

| Variable | Used by | Default | Purpose |
| --- | --- | --- | --- |
| `MSPACE_RUNNER_PORT` | Electron main process | `7788` | Port used when desktop starts the local runner. |
| `MSPACE_RUNNER_URL` | Electron preload/renderer | `http://127.0.0.1:7788` | API base URL exposed to the renderer. |
| `MSPACE_RUNNER_START_TIMEOUT_MS` | Electron main process | `60000` | How long the desktop waits for the runner health check before startup fails. |
| `MSPACE_PORT` | Go runner | `7788` | Port used by a standalone runner. |

Project Kubernetes fields are passed into session commands as:

| Variable | Source |
| --- | --- |
| `MSPACE_KUBE_CONTEXT` | Project `kube_context`. |
| `MSPACE_KUBE_NAMESPACE` | Project `namespace`. |

Session metadata is also passed into the agent command as:

| Variable | Source |
| --- | --- |
| `MSPACE_ISSUE_ID` | Current issue id. |
| `MSPACE_SESSION_ID` | Current session id. |
| `MSPACE_SESSION_BRANCH` | Planned session branch. |
| `MSPACE_SESSION_WORKDIR` | Prepared git worktree path. |
| `MSPACE_SESSION_CONTEXT` | Markdown context file written under `~/.mspace/workdirs/_contexts/`. |

## Smoke Checks

Runner health:

```bash
curl http://127.0.0.1:7788/health
```

Recent sessions:

```bash
sqlite3 ~/.mspace/mspace.db "select id,status,branch,workdir,updated_at from agent_sessions order by updated_at desc limit 5;"
```

Inspect a session worktree:

```bash
git -C ~/.mspace/workdirs/<project-id>/<session-id> status --short
```

Run validation commands:

```bash
pnpm typecheck
pnpm --filter @mspace/desktop build
(cd packages/ui && pnpm dlx shadcn@latest info --json)
(cd runner && go test ./...)
(cd runner && go build ./...)
```

UI component check:

```bash
cd packages/ui && pnpm dlx shadcn@latest info --json
```

Expected shadcn/ui source components currently include:

- alert
- badge
- button
- card
- field
- input
- label
- separator
- textarea

## Common Troubleshooting

### Desktop Shows Unstyled HTML

Tailwind CSS 4 must scan the monorepo package sources and map shadcn semantic tokens. Check that `apps/desktop/src/renderer/src/globals.css` contains:

```css
@import "tailwindcss";
@source "../../../../../packages/ui/src";
@source "../../../../../packages/views/src";

@theme inline {
  --color-background: var(--paper);
  --color-foreground: var(--text);
  --color-card: var(--surface);
  --color-primary: var(--ink);
  --color-border: var(--line);
}
```

The exact token list may grow, but `@theme inline` should keep shadcn color tokens mapped to the mspace palette.

### shadcn Imports Fail During Desktop Build

Check the desktop Vite aliases:

```bash
sed -n '/alias:/,/},/p' apps/desktop/electron.vite.config.ts
```

Required aliases:

- `@mspace/ui/components`
- `@mspace/ui/lib`
- `@mspace/ui`

Then verify both shadcn config files exist:

```bash
test -f components.json
test -f packages/ui/components.json
cd packages/ui && pnpm dlx shadcn@latest info --json
```

### Runner Port Is Already In Use

Check the health endpoint first:

```bash
curl http://127.0.0.1:7788/health
```

If another process owns the port, choose another port:

```bash
MSPACE_RUNNER_PORT=7790 MSPACE_RUNNER_URL=http://127.0.0.1:7790 pnpm dev:desktop
```

For standalone runner debugging:

```bash
cd runner && MSPACE_PORT=7790 go run .
```

### Project Creation Fails

Local-folder projects must point to an absolute git repository path. Check:

```bash
test -d /absolute/path/to/repo/.git && git -C /absolute/path/to/repo status --short
```

If the project is created from the desktop app, prefer the folder picker instead of typing the path manually.

GitHub imports must use a GitHub repository URL. The runner clones them into `~/.mspace/repos/<owner>/<repo>`. Check:

```bash
test -d ~/.mspace/repos/<owner>/<repo>/.git && git -C ~/.mspace/repos/<owner>/<repo> remote -v
```

If issue creation fails immediately after project setup, the usual cause is that no project exists yet or the runner could not resolve the project from the issue prompt. Create the project first or select it explicitly in the issue modal.

### Project Delete Fails

Projects can only be deleted before any issues or sessions are attached. Check:

```bash
sqlite3 ~/.mspace/mspace.db "select id,name,issue_count,session_count from (select p.id,p.name,count(distinct i.id) as issue_count,count(distinct s.id) as session_count from projects p left join issues i on i.project_id = p.id left join agent_sessions s on s.issue_id = i.id group by p.id) order by name;"
```

### Session Fails Before Running Command

The runner creates a git worktree before starting the command. Check:

```bash
git --version
git -C /absolute/path/to/repo worktree list
sqlite3 ~/.mspace/mspace.db "select id,status,branch,workdir from agent_sessions order by updated_at desc limit 5;"
```

Common causes:

- `git` is not on `PATH`;
- the project repo path is not a git repository;
- the session branch already exists in an unexpected state;
- the planned worktree directory already exists.

### Kubernetes Validation Does Not Run

The local MVP does not create scoped kubeconfigs or ServiceAccounts yet. It reuses the configured local kube context and namespace when project deploy or validation commands use `kubectl`.

Check:

```bash
kubectl config current-context
kubectl --context <context> -n <namespace> get pods
```

### Session Detail Has No Diff

The workspace snapshot is read from the session worktree stored in `agent_sessions.workdir`. Check:

```bash
sqlite3 ~/.mspace/mspace.db "select id,workdir from agent_sessions order by updated_at desc limit 1;"
git -C <workdir> status --short
git -C <workdir> diff --stat --patch --find-renames HEAD
```
