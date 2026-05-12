export type ChangelogEntry = {
  date: string;
  title: string;
  summary: string;
  items: string[];
};

export const changelog: ChangelogEntry[] = [
  {
    date: "2026-05-12",
    title: "Workspace automation and namespace resources",
    summary:
      "mspace clarified the delivery loop and made issue test environments easier to inspect after deployment.",
    items: [
      "Added Workspace Settings for automation policy.",
      "Kept source commit capture as an always-on review and deploy anchor.",
      "Added an optional workspace switch for automatic draft PR creation and PR status refresh after source commits.",
      "Added an Issue Detail Resources tab for the current test namespace.",
      "Showed Pods, Services, Deployments, Ingresses, Events, and NodePort mappings without requiring a separate Kubernetes console.",
      "Refined the Evidence tab around the current review packet and compact command evidence.",
      "Moved previous attempts and Kubernetes snapshot history into dedicated full-width evidence pages.",
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
    title: "Desktop MVP and local runner bootstrap",
    summary:
      "The first runnable mspace loop landed: desktop shell, local runner, and agent session foundation.",
    items: [
      "Bootstrapped the mspace desktop app and local Go runner.",
      "Added the local MVP session workflow.",
      "Restored the macOS titlebar drag region.",
    ],
  },
];
