export type ChangelogEntry = {
  date: string;
  title: string;
  summary: string;
  items: string[];
};

export const changelog: ChangelogEntry[] = [
  {
    date: "2026-05-20",
    title: "Local account login for restricted environments",
    summary:
      "mspace added username/password authentication so customer and offline deployments no longer depend on GitHub OAuth for first sign-in.",
    items: [
      "Added password-backed local identities that still issue normal `msp_...` mspace session tokens.",
      "Kept GitHub OAuth as an optional identity provider instead of making it the only sign-in path.",
      "Updated the desktop sign-in surface with local account login and account creation while preserving the GitHub sign-in fallback.",
      "Documented Kubernetes deployments so teams can create a workspace and runtime token without GitHub access.",
    ],
  },
  {
    date: "2026-05-19",
    title: "Kubernetes customer deployment package",
    summary:
      "mspace added the first customer-facing Kubernetes deployment path for the server control plane and fixed Server Worker runtime.",
    items: [
      "Added production server and Codex worker container images for linux/amd64 deployment.",
      "Added a Helm chart for server, Postgres, BuildKit, and the Kubernetes-hosted fixed worker.",
      "Documented the two-stage install flow: deploy server first, then enable the worker after creating a workspace-scoped runtime token.",
      "Kept Codex configuration and authentication mounted only into the worker runtime, while the server stays a Codex-free control plane.",
      "Required worker Codex home Secrets to include both `auth.json` and `config.toml`, with Helm rendering failing fast when worker auth/config keys are missing.",
      "Kept per-session Kubernetes Runtime Provider work deferred while preserving the current server/worker runtime task protocol.",
    ],
  },
  {
    date: "2026-05-19",
    title: "Server-owned runtime surfaces",
    summary:
      "mspace removed the local execution sidecar split and moved the remaining workspace runtime surfaces behind the server control plane.",
    items: [
      "Added server-owned workspace settings, agent profiles, clusters, issue test environments, namespace resources, preview probes, and PR handoff records.",
      "Rerouted desktop Agents, Clusters, test deployment, cleanup, retain, Resources, and PR handoff actions through workspace-scoped server APIs.",
      "Migrated legacy local sessions, session logs, test environments, PR handoffs, agent profiles, cluster settings, and image attachments into server Postgres.",
      "Removed the Electron sidecar startup path, old local execution package, file-database migration, and legacy import script so signed-in workspaces no longer have a second local product store.",
      "Localized Issue Detail Overview, Commits, Resources, Evidence, session/failure timeline controls, and the project runbook, test deploy, and project attachment dialogs.",
      "Kept user-authored issue content, logs, commit hashes, branch names, Kubernetes object names, and runtime protocol values literal so evidence remains inspectable.",
    ],
  },
  {
    date: "2026-05-18",
    title: "Worker guardrails, source branches, and localization",
    summary:
      "mspace tightened Team worker defaults, made PR source branches easier to read from the Commits tab, and added bilingual desktop UI support.",
    items: [
      "Updated server-issued Codex session instructions to prefer lint, tests, typecheck, builds, and short internal probes over starting long-running dev servers.",
      "Prevented Docker worker fallback instructions from presenting container-local `localhost` or `127.0.0.1` URLs as preview links unless the user explicitly asks for a mapped local preview.",
      "Added regression coverage for the server and worker instruction defaults so future sessions keep preview links tied to mspace test environments or known host mappings.",
      "Moved the PR source selector to branch identity and Source branch copy, so multiple commits on one branch do not look like separate PR sources.",
      "Added a Codex session artifact for semantic branch names such as `fix/pr-source-branch-selection`, with runtime normalization before source capture.",
      "Added shared English and Simplified Chinese localization for the desktop shell and main workflow surfaces, with a language switcher in the workspace menu.",
    ],
  },
  {
    date: "2026-05-15",
    title: "Server-owned worker sessions",
    summary:
      "mspace simplified runtime architecture so personal and team workspaces use the server as the single session, task, log, and result source of truth.",
    items: [
      "Moved Issue Detail agent mentions to the server session API for both personal and team workspaces, removing the old local bridge for server-owned worker sessions.",
      "Exposed Runtime registry and queue controls from Workspace Settings for personal and team workspaces, while keeping invitations and shared member controls team-only.",
      "Kept worker logs and returned source metadata in server runtime task records instead of importing worker logs/results into a local store.",
      "Made the Docker dry-run Server Worker script usable from non-interactive shells while preserving the same queue, claim, log, and source-diff loop.",
      "Added a Workspace Settings action that starts the local Docker worker by generating and injecting a short-lived internal bootstrap credential, so local team-mode testing no longer requires copying raw worker tokens.",
      "Added a Codex-capable Docker worker image and startup script that installs the Linux Codex CLI, mounts a dedicated worker Codex home, and advertises `codex:true,dryRun:false` for real Team worker sessions.",
      "Documented the worker authentication boundary so dry-run testing, local Codex credentials, and future managed team runtime credentials stay distinct.",
    ],
  },
  {
    date: "2026-05-14",
    title: "Legacy personal data import",
    summary:
      "mspace added a recovery path for existing local test data after personal workspace product state moved to the server control plane.",
    items: [
      "Added a one-time local data import path so existing personal test issues, comments, labels, reactions, Inbox rows, and project runbooks could be carried into server Postgres.",
      "Documented the import path for development workspaces that still had legacy local product rows before the server cutover.",
      "Connected PG-backed team workspace issues to Team worker sessions through the first bridge, so a shared issue comment could queue an `agent_session` runtime task instead of stopping at collaboration state.",
      "Let users create workspace-level issues before choosing a project, while keeping project attachment required for agent runs, PR handoff, and test environments.",
      "Made issue creation note-first: mspace creates the issue immediately with a draft title, then refreshes the title in the background while keeping manual edits in Issue Detail.",
      "Fixed server-backed issue type triage so new issues move out of the `Classifying` state once the classifier applies or fails.",
      "Made project attachment explicit from Issue Detail, including creating and attaching a GitHub-backed project from a repository URL found in the issue note.",
      "Pinned team workspace agent turns to Team worker execution while preparing the server-owned session path.",
      "Refreshed the README and public website with a curated set of current running screenshots instead of publishing every Issue Detail tab.",
    ],
  },
  {
    date: "2026-05-13",
    title: "Personal and team workspace split",
    summary:
      "mspace made team collaboration explicit so local single-user work and shared team execution no longer blur together.",
    items: [
      "Made GitHub sign-in land in a personal workspace by default.",
      "Moved workspace projects, runbooks, issues, child tasks, comments, reactions, labels, and Inbox receipts into the server control plane for both personal and team workspaces.",
      "Kept the old local store focused on runtime state, attachments, evidence, test environments, PR handoff, and execution metadata while server ownership was still being staged.",
      "Made Issues, Projects, Project runbooks, global search, and Issue Detail use server workspace APIs for signed-in workspaces.",
      "Moved team workspace creation into the left workspace menu beside workspace switching.",
      "Limited Team worker routing, invitations, runtime registration, workers, and task queues to team workspaces before the server-owned runtime queue was generalized.",
      "Updated the workspace menu so users can see whether they are operating in a Personal or Team workspace.",
    ],
  },
  {
    date: "2026-05-12",
    title: "Workspace automation and namespace resources",
    summary:
      "mspace clarified the delivery loop and made issue test environments easier to inspect after deployment.",
    items: [
      "Added Workspace Settings for automation policy.",
      "Added workspace member lists, one-time invite links, invite acceptance, and workspace switching so team collaboration can be tested from the UI.",
      "Added the first Team Runtime worker registry in the control plane.",
      "Exposed Team Runtime tokens, worker status, and task queue records in Workspace Settings.",
      "Added a server-side runtime task queue with worker claim, status, event, and log APIs.",
      "Added the first standalone Team Runtime worker daemon for registration, heartbeat, task claim, protocol smoke completion, and Codex app-server task execution.",
      "Made runtime task events and worker logs inspectable from Workspace Settings.",
      "Let Issue Detail route an agent turn to a Team worker.",
      "Added cooperative Team Runtime cancellation from the issue session stop action through the control plane to the worker.",
      "Clarified the Team Runtime roadmap around fixed Server Workers and future Kubernetes Runtime Providers sharing the same runtime task protocol.",
      "Moved Server Worker sessions toward fixed-server execution by letting workers prepare their own repository workspaces and return source commit metadata.",
      "Added a Docker-based Server Worker simulation path for UI testing without using the local development machine environment.",
      "Kept source commit capture as an always-on review and deploy anchor.",
      "Added an optional workspace switch for automatic draft PR creation and PR status refresh after source commits.",
      "Added an Issue Detail Resources tab for the current test namespace.",
      "Showed Pods, Services, Deployments, Ingresses, Events, and NodePort mappings without requiring a separate Kubernetes console.",
      "Refined the Evidence tab around the current review packet and compact command evidence.",
      "Moved previous attempts and Kubernetes snapshot history into dedicated full-width evidence pages.",
      "Added the mspace browser tab icon using the public website brand mark.",
    ],
  },
  {
    date: "2026-05-11",
    title: "Website, runbooks, review evidence, and session recovery",
    summary:
      "The public website shipped, project runbooks became part of the issue loop, and runner recovery became safer after restarts.",
    items: [
      "Launched the public mspace brand website.",
      "Added the public changelog surface for task-by-task progress logs.",
      "Moved the changelog into its own navigation tab instead of showing it on the homepage.",
      "Refined the website navigation logo lockup so the brand mark feels more deliberate.",
      "Rebuilt the navigation logo artwork as a transparent mark inside the gray-white tile.",
      "Added project runbooks and issue turn controls.",
      "Added the Issue Detail runbook viewer and comment reactions.",
      "Refined issue status, session evidence, and review handoff behavior.",
      "Compacted review evidence commands so evidence stays readable.",
      "Added commit-backed deploy source selection and automatic preview status checks.",
      "Added issue-level branch / PR handoff records with PR creation, branch-based PR detection, and status refresh from Issue Detail.",
      "Made failed sessions and failed deploy checks visible as continueable issue evidence.",
      "Expanded Issue Detail review tabs by hiding the metadata sidebar outside Overview.",
      "Limited manual issue lifecycle actions to the intended human workflow.",
      "Synced issue workflow guidance across project docs.",
      "Reconciled orphaned active sessions on runner startup.",
      "Replaced stale local server processes during desktop startup.",
    ],
  },
  {
    date: "2026-05-10",
    title: "Inbox receipts, authenticated review flow, and attachments",
    summary:
      "Team review state moved closer to the control plane while issue comments gained richer evidence inputs.",
    items: [
      "Added command palette search for issues and projects.",
      "Documented the product value thesis.",
      "Added team-level unread receipts for Inbox review updates.",
      "Removed the redundant issue unread marker from issue rows.",
      "Switched changed-file displays to Material file icons.",
      "Added authenticated issue review status workflow.",
      "Deduped control-plane inbox issue events.",
      "Added image attachment support for issue notes and comments.",
    ],
  },
  {
    date: "2026-05-09",
    title: "Clusters, issue test environments, labels, and GitHub sign-in",
    summary:
      "mspace gained the first complete shape of issue-scoped Kubernetes validation and richer collaboration state.",
    items: [
      "Prevented the agent summary loading flash.",
      "Added TanStack Router navigation controls.",
      "Defaulted local project names from imported folders.",
      "Added reusable clusters and issue test environments.",
      "Structured the Kubernetes evidence panel.",
      "Added file type icons to session change surfaces.",
      "Anchored the agent mention menu to the text caret.",
      "Added issue task lists and label triage.",
      "Added rich comments and task deletion.",
      "Added GitHub control-plane sign-in.",
    ],
  },
  {
    date: "2026-05-08",
    title: "Managed agents, issue sessions, and product presentation",
    summary:
      "The MVP became easier to recognize and explain: agents became manageable product objects and the README gained real screenshots.",
    items: [
      "Added managed agents and the issue session flow.",
      "Added the mspace app logo.",
      "Centered the mspace logo mark in the desktop shell.",
      "Added screenshots and a clearer project overview to the README.",
    ],
  },
  {
    date: "2026-05-07",
    title: "Notion-style workspace, project import, and roadmap",
    summary:
      "The product shell moved into a quiet document-workspace direction while issue intake and project import became concrete.",
    items: [
      "Adopted a shadcn/ui Notion-style workspace surface.",
      "Documented the shadcn Notion UI system.",
      "Refined the Notion-inspired design system.",
      "Added the product roadmap.",
      "Added issue triage and project import flow.",
      "Synced issue and project workflow docs.",
    ],
  },
  {
    date: "2026-05-06",
    title: "Desktop MVP and local execution bootstrap",
    summary:
      "The first runnable mspace loop landed: desktop shell, local execution, and agent session foundation.",
    items: [
      "Bootstrapped the mspace desktop app and local Go runner.",
      "Added the local MVP session workflow.",
      "Restored the macOS titlebar drag region.",
    ],
  },
];
