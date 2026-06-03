# mspace Design System

> Status: design system baseline, updated 2026-06-03

## Visual Thesis

mspace should feel like a clean black-and-white Notion-style document workspace for serious agent work: white paper surfaces, a barely gray navigation rail, compact operational rows, monochrome icons, and Kubernetes evidence attached to the issue rather than competing with it.

The closest visual reference is Notion's calm document environment. The product interaction reference remains Multica-style inbox, issue, and teammate collaboration. The runtime evidence should feel closer to Optio's Kubernetes-backed proof of work.

Scope boundary: these rules govern the desktop product shell and shared app UI. The public website in `apps/website` is allowed to be a high-contrast brand surface, but it should remain recognizably about the issue-to-evidence loop and must not leak its marketing treatment back into product routes.

## Design Principles

- Document first: Issue Detail is the main working surface, not a dashboard drill-down.
- Operationally quiet: prioritize legibility, scan speed, and stable layout over decorative expression.
- Evidence attached: logs, diffs, branch state, and Kubernetes status should support the issue story.
- Worker-backed, K8s-aware: UI copy should make the selected runtime worker and validation namespace explicit.
- User-centered disclosure: show what the user needs to decide, trust, or do next. Do not expose raw tokens, ids, protocol statuses, internal timestamps, or implementation labels when the user only needs the product meaning.
- Automatic over manual: if the product can infer or carry a value from a link, session, workspace, server health, or selected issue, do not make the user paste, choose, or configure it.
- Compact but not cramped: dense rows are good; tiny hit areas and ambiguous icons are not.
- No marketing shell inside the product: no hero sections, decorative dashboards, abstract AI claims, or generic landing-page composition in desktop routes.

## CSS Strategy

Use Tailwind CSS 4 as the single styling system for product UI.

- Global CSS and token mapping live in `apps/desktop/src/renderer/src/globals.css`.
- shadcn semantic tokens are mapped through `@theme inline`.
- Monorepo sources must stay visible to Tailwind through `@source` entries for `packages/ui/src` and `packages/views/src`.
- Do not introduce CSS Modules, styled-components, emotion, or a second icon library for normal product UI. The only scoped exception is Material Icon Theme for file-type surfaces.
- Avoid dynamically constructed Tailwind class names for theme colors. Use static classes or CSS variables.

## Component Source

The canonical component source is shadcn/ui in `packages/ui/src/components/ui`.

Current installed shadcn/ui primitives:

- `alert`
- `badge`
- `button`
- `card`
- `dropdown-menu`
- `field`
- `input`
- `label`
- `scroll-area`
- `separator`
- `select`
- `switch`
- `textarea`

Project-facing wrappers, app shell components, and compatibility exports live in `packages/ui/src/index.tsx` and are consumed through `@mspace/ui`.

Global shell controls should use the existing shadcn primitives instead of custom facades. The current workspace-menu language switcher uses shadcn `DropdownMenu` so the trigger, menu item density, focus behavior, and keyboard interaction match the rest of the app shell.

When adding a shared primitive:

1. Check existing components first.
2. Add new primitives with `pnpm dlx shadcn@latest add <component>` from `packages/ui`.
3. Keep generated source close to shadcn's source shape.
4. Export product-facing names from `@mspace/ui` only when the app actually needs them.
5. Preserve the desktop aliases in `apps/desktop/electron.vite.config.ts`:
   - `@mspace/ui/components`
   - `@mspace/ui/components/ui`
   - `@mspace/ui/lib`
   - `@mspace/ui`

## Color Palette

The palette is a Notion-style black-and-white light system. It should read as clean white paper, graphite text, barely gray navigation, and very restrained semantic status color only when state requires it.

| Token | Current value | Role |
| --- | --- | --- |
| `--canvas` | `#ffffff` | Outer app background. |
| `--sidebar` | `#f7f7f7` | Left navigation surface. |
| `--paper` | `#ffffff` | Main document page. |
| `--surface` | `#ffffff` | Panels and cards. |
| `--block` | `#f7f7f7` | Subtle grouped content blocks. |
| `--hover` | `#eeeeee` | Hover fill. |
| `--selection` | `#eeeeee` | Active navigation and selected rows. |
| `--line` | `rgba(0, 0, 0, 0.1)` | Dividers and low-contrast outlines. |
| `--ink` | `#1f1f1f` | Primary action and mark color. |
| `--text` | `#1f1f1f` | Primary text. |
| `--muted` | `#737373` | Secondary text. |
| `--faint` | `#a1a1a1` | Hints, section labels, quiet icons. |
| `--accent` | `#1f1f1f` | Focus and primary interaction accent. |
| `--accent-blue` | `#3f6ea8` | Informational runtime signal only. |
| `--inbox-badge` | `#e5f0ff` | Sidebar Inbox unread-count badge background. |
| `--inbox-badge-text` | `#2f5f9e` | Sidebar Inbox unread-count badge text. |
| `--inbox-unread-dot` | `#5f98d1` | Inbox row unread dot. |
| `--success` | `#2f6f4e` | Healthy or completed state only. |
| `--done` | `#8250df` | GitHub-style closed issue state only. |
| `--warning` | `#8a6500` | Blocked or needs attention only. |
| `--danger` | `#b3261e` | Destructive or failed state only. |
| `--focus` | `rgba(0, 0, 0, 0.24)` | Focus ring. |
| `--code-bg` | `#202020` | Code and terminal background. |

Color rules:

- Keep almost all UI weight in white, black, and neutral gray.
- Use accent colors for state and action, not decoration.
- Inbox unread indicators use the dedicated light-blue token set above. The sidebar count badge should not have a border or outer ring.
- Sidebar icons and namespace markers stay monochrome; do not color navigation chrome.
- Do not introduce purple-to-blue AI gradients.
- Do not reintroduce warm beige, cream, parchment, or yellow-tinted surfaces.
- Status badges should use semantic variants or shared token classes, not ad hoc raw colors.

## Typography

Current product font stack:

```css
-apple-system, BlinkMacSystemFont, "Segoe UI", Helvetica, Arial, sans-serif
```

Typography should feel like Notion's conservative system UI: readable, compact, steady, and intentionally un-fancy.

| Role | Size | Weight | Line height | Notes |
| --- | --- | --- | --- | --- |
| Page title | `32px` | `600` | `1.1` | Use only for top-level page headings. |
| Section title | `13px-16px` | `600` | `1.4-1.5` | Match panel density; avoid oversized headings in compact views. |
| Body | `13px-14px` | `400` | `1.55-1.7` | Good for issue documents, comments, and metadata. |
| Small metadata | `11px-12px` | `400-500` | `1.35-1.5` | Use for timestamps, namespace hints, labels. |
| Code/logs | `12px-13px` | `400` | `1.55` | Use tabular numbers where values update or align. |

Typography rules:

- Use sentence case for headings and labels.
- Avoid all-caps except tiny section labels with generous letter spacing.
- Do not use decorative product fonts such as Avenir for the app shell.
- Do not scale font size with viewport width.
- Keep letter spacing at the default; avoid negative tracking for page titles.
- Use `text-pretty` for longer descriptions and `truncate` for row metadata.
- Use tabular numbers for counters, timestamps, resource quantities, and log metadata.

## Radius And Surfaces

Radius scale:

- `4px`: small controls and inner details.
- `7px`: default buttons, inputs, navigation items.
- `10px`: panels and compact cards.
- `12px`: larger grouped surfaces.

Surface rules:

- Use background-color steps before heavy borders.
- Use shadows lightly: just enough to separate paper from canvas.
- Do not place UI cards inside other UI cards.
- Use `Separator` for structural dividers instead of ad hoc borders.
- Keep repeated list rows stable in height; hover and active states must not shift layout.

## Layout

The default desktop shape is a sidebar plus document workspace.

- Sidebar width: about `252px`.
- Main content: centered page frame with a max width near `1280px`.
- Page padding: generous enough for reading, tight enough for operations.
- Default routes in the app shell: Inbox, Issues, Tests, Agents, Clusters, and Projects. Issue, Test Case, Test Plan, Test Run, and Session details are reached from objects.
- Object list pages must not use a left-list/right-detail split. Clicking a row opens a dedicated detail page with its own route and breadcrumbs. Inline list pages may use filters, batch actions, and creation/import panels, but not persistent side-by-side detail panes.

Screen priorities:

- Inbox: row-level triage, unread state, assignment, linked session.
- Issue Detail: document body first, then activity, session, and evidence. Keep the right metadata sidebar on Overview only; Commits, Sessions, and Evidence use the full page width so diffs, paths, command output, and Kubernetes evidence have room.
- Tests: project-level cases, case suggestions, plans, and runs. Creation/import can use focused modals; row details should open dedicated pages rather than a persistent list/detail split.
- Session Detail: logs, worktree state, branch comparison, evidence.
- Agents: managed Codex-backed profiles, mentions, enabled state, and instructions.
- Clusters: reusable kubeconfig, registry, and preview exposure defaults.
- Projects: repository and runtime policy, not a daily conversation feed.

## Components

### Buttons

- Use shadcn `Button` variants through `@mspace/ui`.
- Icons inside buttons use lucide with `data-icon`.
- Icon-only buttons need `aria-label`.
- Active press should use a subtle scale, currently `active:scale-95` or nearby.
- Prefer icon buttons for familiar tools; use text buttons for clear commands.

### Forms

- Use `Field`, `FieldLabel`, `FieldDescription`, `Input`, and `Textarea`.
- Keep labels short and specific.
- Validation states use `data-invalid` on the field and `aria-invalid` on the control.
- Project configuration should keep creation light, then expose project name, repository metadata, default cluster, and the project runbook from a full settings page. Do not add separate install, test, build, deploy, or validation command fields; the runbook is the user-facing operation knowledge surface. Cluster settings own kubeconfig, context, registry, and preview exposure defaults.
- The issue creation, comment, and project runbook editors use a local TipTap view component, not a shared shadcn primitive. Keep `.mspace-doc-editor` styling quiet, document-like, and Markdown-compatible. The Issue Detail runbook modal should use the `runbook-viewer` read-only TipTap variant with light code blocks, not the editable runbook shell or a ReactMarkdown fallback. Image attachment nodes should render as restrained thumbnails with a stable loading/error fallback; never expose browser-default broken-image text as the primary UI.
- Comment reactions should stay visually secondary: quiet chips for existing reactions, a small SmilePlus-style emoji trigger instead of a generic plus icon, and no heavy bordered toolbar around the comment body.

### Cards And Panels

- Use cards for repeated objects, panels, and modal-like grouped content.
- Do not use cards as generic page sections.
- Cards should have a real job: issue row, evidence block, session summary, project row, or config panel.

### Badges And Status

- Use `Badge` for status, branch/runtime labels, and namespace hints.
- Status storage values remain English, but visible status labels follow the active product locale.
- Visible product copy follows the active locale through `@mspace/i18n`; technical identifiers, logs, paths, Kubernetes object names, commit hashes, branch names, and user-authored content stay literal unless the product explicitly decides otherwise.
- Status badges must use readable labels, not storage values: `Needs review`, `Ready for test`, and `Closed` are acceptable; `needs_review` and `ready_for_test` are not. Runtime badges may still use readable progress labels such as `Running` or `Deploying`.
- Use GitHub-adjacent status semantics for issues: `Open` is green and `Closed` is purple. Other handoff states should use restrained semantic colors that help scanning without turning the issue header or timeline into a dashboard. The Issue sidebar status is a read-only badge. Human lifecycle actions belong inside the Issue Detail comment composer footer: keep the primary action visible, hide less common close reasons such as `Close as not planned` in a compact dropdown, and do not repeat the current issue status there. Do not use a generic Issue status selector, and do not put transient session or test-deploy progress there.
- Status-change timeline rows should be one-line actor events with from/to badges. Hide compatibility prose from the stored comment body unless the user is inspecting raw data.
- Avoid decorative badges that do not change behavior or scanning. In the Issue Resources tab, prefer static semantic icons, counts, and resource facts over repeating status pills next to every Kubernetes object name.

### Alerts And Empty States

- Use `Alert` for real warnings, blockers, and setup problems.
- Empty states should tell the next useful action, not explain the whole product.

## Icons

Use `lucide-react` for normal product UI. File-type chips and file-change rows use `material-icon-theme` SVG assets so they match the IDE-style file type icons users expect from VS Code-like file trees.

- Keep icon stroke consistent with the global `svg` rule.
- Do not add manual `size-*` classes inside shadcn buttons unless the component requires it.
- Prefer concrete icons: Inbox, Folder, Terminal, Git branch, Logs, Check, Alert, Clock.
- Keep Material Icon Theme scoped to file surfaces through `packages/views/src/file-type-icon.tsx`; do not reintroduce MUI or Emotion just for file icons.
- Hide directory-only placeholder entries from changed-file lists; show the concrete files inside those directories instead.
- Avoid abstract sparkle or AI icons except where the runtime worker or agent identity needs a quiet hint.

## Motion

Motion should make the app feel responsive, not theatrical.

- Hover: color or background shift, no layout movement.
- Press: subtle scale.
- Icon swaps: short opacity/scale cross-fade if needed.
- Page transitions: optional and minimal; never block reading or log streaming.
- Use explicit transition properties, not `transition-all`.
- Honor `prefers-reduced-motion`.

## Copy And Tone

Write like an engineering tool used during real work.

- Prefer concrete nouns: issue, session, worktree, branch, namespace, evidence.
- Avoid vague AI words: seamless, unlock, elevate, next-gen, magic.
- Error copy should say what failed and what to try.
- Empty states should name the next command or action.
- Keep Kubernetes context visible when it affects what the agent can do.

## Accessibility

- Every interactive target should be at least `40px` in hit area.
- Icon-only actions require `aria-label`.
- Navigation uses links; actions use buttons.
- Preserve visible focus states through `--focus` and shadcn focus rings.
- Keep contrast strong enough for long reading sessions.
- Do not rely on color alone for destructive, warning, or success states.

## Do And Don't

Do:

- make issue pages feel like durable working documents;
- keep operational metadata compact and aligned;
- use shadcn primitives before custom markup;
- use OKLCH tokens from `globals.css`;
- make runtime, namespace, branch, and evidence easy to scan.

Don't:

- build marketing heroes inside the app;
- add decorative gradient backgrounds;
- create card grids where rows would scan better;
- mix icon libraries outside the explicit lucide product UI plus Material Icon Theme file-surface boundary;
- use raw Tailwind colors for semantic state;
- hide Kubernetes scope behind vague "environment" copy when namespace matters.

## Verification

Run these checks after design-system or shared UI changes:

```bash
pnpm typecheck
pnpm --filter @mspace/desktop build
(cd packages/ui && pnpm dlx shadcn@latest info --json)
```

For significant UI changes, also run the desktop app and inspect Inbox, Projects, Issue Detail, and Session Detail at desktop and narrow widths.
