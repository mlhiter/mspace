export type ChangelogEntry = {
  date: string;
  title: string;
  summary: string;
  items: string[];
};

export const changelog: ChangelogEntry[] = [
  {
    date: "2026-06-05",
    title: "Workspace-level test plans and runs",
    summary:
      "mspace made Tests able to orchestrate one plan across multiple projects while keeping cases anchored to the repositories that execute them.",
    items: [
      "Added workspace-level plan and run APIs so a test plan can include ready cases from more than one project in the same workspace.",
      "Kept test cases and case suggestions project-scoped, with each plan case and run item preserving the project that owns execution context.",
      "Grouped test execution by project when a run spans repositories, so each Issue-backed agent session still runs against one project workdir.",
      "Updated the desktop Tests surface to list workspace plans and runs while keeping the project selector for Cases and Case suggestions.",
    ],
  },
  {
    date: "2026-06-05",
    title: "Archived deletion and pagination for test cases",
    summary:
      "mspace made large test-case libraries easier to manage without destroying historical test evidence.",
    items: [
      "Changed the Tests case list to use server-backed `limit` and `offset` pagination with total counts.",
      "Added single-case and selected-case archive actions so users can remove cases from normal browsing in bulk.",
      "Kept archived cases, revisions, run history, plan references, and evidence readable instead of hard-deleting records.",
      "Updated the API contract so clients can inspect archived cases with `status=archived` while default case browsing hides them.",
    ],
  },
  {
    date: "2026-06-05",
    title: "Worker-assisted column mapping for case imports",
    summary:
      "mspace moved unfamiliar case-import headers out of hardcoded parser aliases and into reviewable worker suggestions.",
    items: [
      "Added a `test_case_import_mapping` runtime task so Codex-capable workers can suggest source-column mappings from headers and sample rows.",
      "Kept the server parser focused on canonical fields plus explicit confirmed `columnMappings` instead of growing language-specific aliases.",
      "Showed worker confidence and reasons in the import preview, then refreshed the preview before the user confirms the final import.",
      "Mapped business categories to tags and historical execution-state columns to `latest_result` when the worker suggests that structure.",
    ],
  },
  {
    date: "2026-06-05",
    title: "Preview-before-confirm case imports",
    summary:
      "mspace made larger test-case files safer to import by parsing them on the server first and asking the user to confirm before writing cases.",
    items: [
      "Added a read-only import preview API for Markdown, text, CSV, and Excel `.xlsx` files.",
      "Showed importable count, skipped rows, missing field counts, quality findings, sample cases, and skipped-row samples in the Tests import modal.",
      "Raised the backend import guardrails to 1,000 cases per request and 2 MB content/workbook files, with the HTTP body limit sized for base64 Excel uploads.",
      "Kept the final import call separate so users can inspect parsed results before creating project test cases.",
    ],
  },
  {
    date: "2026-06-05",
    title: "File-based test case imports",
    summary:
      "mspace made every test-case import format use the same choose-file interaction instead of mixing paste fields with workbook upload.",
    items: [
      "Changed Markdown, text, and CSV imports to use file selection alongside Excel `.xlsx` workbooks.",
      "Added per-format file filtering and validation for `.md`, `.markdown`, `.txt`, `.text`, `.csv`, and `.xlsx` files.",
      "Kept the server import contract unchanged: text-like files are read as content, while Excel workbooks still send base64 content.",
      "Updated English and Simplified Chinese import copy so the modal now talks about choosing files instead of pasting content.",
    ],
  },
  {
    date: "2026-06-05",
    title: "Plan-level setup for test runs",
    summary:
      "mspace made formal test plans handle real preconditions before case execution, without turning setup into a heavy template system.",
    items: [
      "Added optional setup steps to test plans for work such as confirming the target environment, updating a deployment image, SSHing into a VM, logging into a platform, or preparing a preview URL.",
      "Made plan-based runs create one setup Issue and agent session first, keeping case items queued until setup passes.",
      "Added `test-setup-result.json` reconciliation so setup outputs such as preview URL, image, namespace, or SSH target become run context for later case execution.",
      "Stopped case execution when setup fails, is cancelled, or omits the setup artifact, so failed preconditions do not produce misleading test results.",
      "Surfaced setup status, setup steps, setup result, and run context in the Tests UI and localized the new copy in English and Simplified Chinese.",
    ],
  },
  {
    date: "2026-06-05",
    title: "Virtual machine environment rechecks",
    summary:
      "mspace made SSH virtual machine targets re-verifiable from the Environments list instead of requiring users to reopen settings.",
    items: [
      "Added an Environment-level check API that refreshes Kubernetes reachability through the existing kubeconfig path and virtual machine reachability through saved server-side SSH credentials.",
      "Added the missing Check action to virtual machine rows in Environments, matching the Kubernetes row affordance.",
      "Let VM checks use an already saved SSH credential directly, with an optional dialog path to replace the credential and recheck.",
      "Persisted VM SSH passwords or private keys server-side for future worker access while keeping raw credential material out of Environment responses.",
    ],
  },
  {
    date: "2026-06-05",
    title: "Select-all controls for test cases",
    summary:
      "mspace made the Test Case library easier to use when starting batch optimization, plans, or selected-case runs.",
    items: [
      "Added a Select all action for the current filtered case list so users can quickly prepare a batch from search or status results.",
      "Added a header checkbox with checked, unchecked, and mixed states to match the row-selection model users expect in a table.",
      "Kept Select ready and Clear available as separate batch actions so runnable-case selection remains explicit.",
      "Localized the new selection controls and screen-reader labels for English and Simplified Chinese.",
    ],
  },
  {
    date: "2026-06-04",
    title: "Issue-style test case detail layout",
    summary:
      "mspace made Test Case Detail read more like Issue Detail, with page-level actions and tabs instead of a repeated inner header.",
    items: [
      "Moved Run case and Save case actions into the page header so they stay aligned with the object title.",
      "Removed the duplicated case title summary block below the page title while keeping project, executability, status, and type as compact metadata.",
      "Moved Details, Run history, and Revisions navigation directly under the title area with the same quiet tab treatment as Issue Detail.",
      "Kept save behavior intact by binding the header Save action back to the detail form.",
    ],
  },
  {
    date: "2026-06-04",
    title: "Environments for Kubernetes and VM targets",
    summary:
      "mspace evolved the Clusters module into Environments so test and deployment work can target Kubernetes clusters or SSH-managed virtual machines.",
    items: [
      "Added workspace Environment APIs and UI navigation while keeping the old Kubernetes cluster APIs as compatibility records.",
      "Made Kubernetes environments project from kubeconfig-backed cluster records and added virtual machine environments with SSH host, user, port, workdir, service hint, labels, and SSH credential configuration state.",
      "Added a Kubernetes Environment reachability check that refreshes status from the kubeconfig, selected context, API server, and namespace list permission.",
      "Added SSH password and private-key validation for virtual machine environments so VMs are only marked verified after a real login check.",
      "Let test plans and test runs select an Environment and freeze environment id, kind, and snapshot at creation time so historical runs keep their target context.",
      "Kept workers decoupled from environments: workers claim runtime tasks, then operate the selected target through Kubernetes, SSH, or future provider-specific access.",
      "Renamed the team worker setup language around worker hosts and install commands so workers are not confused with validation targets.",
      "Kept issue deploy, cleanup, Resources, and preview probing Kubernetes-only for this slice, rejecting VM environments instead of pretending namespace workflows can run there.",
    ],
  },
  {
    date: "2026-06-03",
    title: "Direct selected-case test runs",
    summary:
      "mspace made test execution start from selected ready cases without forcing users to create a formal plan first.",
    items: [
      "Added project-level test run creation for selected ready cases while keeping plan-based runs for formal release passes.",
      "Made test runs record their source so selected-case, plan, retry, and later incremental runs can share the same Issue-backed execution model.",
      "Updated the Tests UI so the Run stage lists project run history directly and offers both selected-case runs and plan runs.",
      "Kept worker preflight, parent Issues, child execution Issues, agent sessions, `test-result.json` reconciliation, and human acceptance in the same auditable path.",
    ],
  },
  {
    date: "2026-06-03",
    title: "Readable case revision history",
    summary:
      "mspace made Test Case revision history show what changed instead of listing only version titles.",
    items: [
      "Added field-level revision summaries on the Test Case detail page, comparing each revision snapshot against the previous version.",
      "Showed compact before/after values for changed fields such as title, type, priority, status, steps, expected result, environment, tags, and executability.",
      "Kept the initial revision readable with an initial-version marker and key case facts.",
      "Localized the revision summary copy for English and Simplified Chinese.",
    ],
  },
  {
    date: "2026-06-02",
    title: "Runnable test case guidance",
    summary:
      "mspace made the Tests case form explain what makes a test case executable instead of leaving users to infer backend format rules.",
    items: [
      "Added a runnable-case checklist to the shared create and edit form for title, preconditions, steps, expected result, and environment requirements.",
      "Added a test case type field for functional, UI, API, and deployment cases so the case detail page no longer implies every case is functional-only.",
      "Added field-level examples and hints so users understand priority, status, concrete steps, and environment requirements before saving.",
      "Localized quality findings into actionable labels and clarified Markdown, text, and CSV import expectations.",
      "Added Excel `.xlsx` import for project test cases, using the same title, type, area, priority, preconditions, steps, expected result, environment, and tags columns as CSV.",
    ],
  },
  {
    date: "2026-06-02",
    title: "Workspace-aware project source selection",
    summary:
      "mspace clarified where project source comes from when a user is working in a shared team workspace.",
    items: [
      "Kept the local folder picker available for personal desktop projects.",
      "Changed team project creation to use GitHub repository URLs only, because connected team workers clone source into their own repo cache instead of reading a user's Mac-local path.",
      "Added server-side validation so API requests cannot create a team project backed by a desktop-local folder.",
    ],
  },
  {
    date: "2026-06-01",
    title: "One-step Helm worker bootstrap",
    summary:
      "mspace made customer Helm installs create a default team workspace and register the fixed Kubernetes worker without asking operators to copy a runtime token from the UI.",
    items: [
      "Added server bootstrap support for creating an admin-owned team workspace and registering a Helm-managed `msw_...` runtime token.",
      "Updated the Helm chart so the server and worker share the same runtime token Secret while Codex auth/config remains worker-only.",
      "Added a sanitized Kubernetes worker `config.toml` template so operators do not copy laptop-local Codex settings into the cluster.",
      "Updated Kubernetes deployment docs to make the fixed worker path a one-step install after the operator prepares worker Codex auth.",
    ],
  },
  {
    date: "2026-05-28",
    title: "Deep-link team invites",
    summary:
      "mspace made private-deployment team invitations work with local username/password accounts instead of pretending every teammate has a verified email address.",
    items: [
      "Changed Workspace Settings from email-style invites to one-time join links.",
      "Added a safe unauthenticated invite preview that exposes only workspace name, role, inviter display fields, expiry, and status.",
      "Registered the desktop `mspace://invite/<token>?server=<team-server-url>` protocol so invite links can open the app directly and keep the team server context.",
      "Made signed-out invite recipients create or sign into a local account before the app automatically accepts the invite and switches to the team workspace.",
    ],
  },
  {
    date: "2026-05-27",
    title: "Worker host install flow",
    summary:
      "mspace moved team worker setup from manual credential handling to a self-host install command that connects a worker host to the server queue.",
    items: [
      "Added a server endpoint that creates a short-lived worker install command for workspace owners and admins.",
      "Added a `/install/worker` script route that starts the Docker-backed Codex worker on the target host.",
      "Changed Workspace Settings to lead with `Connect environment` instead of manual token creation.",
      "Added team workspace identity editing from Workspace Settings for owners and admins, covering name, selectable mark, and description.",
      "Kept worker credential rows as audit history while hiding raw bootstrap credentials from the normal product path.",
      "Updated runtime docs so self-hosted workers use the install-command flow and raw `msw_...` tokens remain a debug/recovery path.",
    ],
  },
  {
    date: "2026-05-27",
    title: "Clearer workspace runtime settings",
    summary:
      "mspace made Workspace Settings explain worker credentials and runtime tasks in product language instead of exposing only low-level protocol records.",
    items: [
      "Separated active worker credentials from expired or replaced credential history so desktop personal worker renewals no longer look like duplicate manual tokens.",
      "Labeled desktop-managed personal worker credentials as automatic credentials with metadata that explains they are managed by the desktop worker lifecycle.",
      "Renamed the task queue surface to runtime tasks, added an Issue-title column, and linked issue-backed tasks back to the relevant Issue Detail session.",
      "Paginated runtime tasks in Workspace Settings so busy workspaces can move through task history without relying on one long scrolling table.",
      "Kept protocol kind, capabilities, payload, result, events, and logs available in task details for debugging without making raw runtime fields the primary row content.",
      "Removed the manual queue-task button from the normal runtime settings surface and replaced final-state cancel actions with detail controls.",
    ],
  },
  {
    date: "2026-05-26",
    title: "Source-aware desktop sign-in",
    summary:
      "mspace made the desktop sign-in screen match whether the app is using the local personal server or an explicitly configured team server.",
    items: [
      "Opened the default local desktop sign-in flow on account creation so first-time personal users do not have to switch tabs.",
      "Hid GitHub sign-in for the default local personal server even when a development server advertises OAuth support.",
      "Kept GitHub sign-in available for explicitly configured team servers when `/health` reports OAuth as enabled.",
      "Reset the local-account form mode when switching between the default local server and a saved or environment-provided team server.",
      "Added an opt-in workspace setting that automatically queues an issue test-environment deployment after a source session captures a commit.",
      "Blocked agent mentions when the selected workspace has no active matching Codex worker, avoiding sessions that sit in the queue waiting for a worker that does not exist.",
      "Added automatic host-local personal worker startup for desktop personal workspaces while keeping team workspaces on explicit registered team workers.",
      "Fixed default worker session branch names so repeated personal sessions under the same issue no longer collapse to the same `session-` branch.",
    ],
  },
  {
    date: "2026-05-25",
    title: "Desktop team server selection",
    summary:
      "mspace made customer and team deployments easier to reach from the desktop app without relying only on launch-time environment variables.",
    items: [
      "Added a collapsed Team server entry on the sign-in screen for testing and saving a remote control-plane URL before authentication.",
      "Persisted the selected server in the Electron user-data profile while keeping `MSPACE_SERVER_URL` as the highest-priority launch override.",
      "Made GitHub sign-in appear only when the active server reports OAuth as configured, keeping local-account login clean for offline and customer setups.",
      "Updated local Docker worker startup so workers follow the active desktop server instead of assuming the local control plane.",
      "Documented that personal desktop mode stays local by default while team deployments can opt into a saved server URL.",
    ],
  },
  {
    date: "2026-05-21",
    title: "Personal registration with team runtime gates",
    summary:
      "mspace kept open account registration while tightening team workspace and server runner access around explicit admin invitations.",
    items: [
      "Added a server-admin login allowlist for creating team workspaces in deployed environments.",
      "Kept self-registered users in personal workspaces until a team owner/admin invites them.",
      "Enforced runtime mode boundaries so personal workspaces use personal workers and team workspaces use team workers.",
      "Updated Workspace Settings and deployment docs so worker credentials and team runtime setup stay owner/admin scoped.",
    ],
  },
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
      "Added a Workspace Settings action that starts the local Docker worker by generating and injecting a short-lived internal bootstrap credential, so local team-mode testing no longer requires copying raw worker credentials.",
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
