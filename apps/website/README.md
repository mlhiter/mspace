# mspace Website

This app is the public brand site for mspace. It is a static Vite/React/Tailwind site that tells the issue-to-evidence story: issue document, Codex session, source change, Kubernetes namespace preview, review evidence, and cleanup decision.

The site has two navigation views:

- `Home`: the issue-to-evidence brand narrative;
- `Changelog`: a static day-level build log backed by `src/changelog.ts`.

Production site: [mspace-website-blue.vercel.app](https://mspace-website-blue.vercel.app)

## Commands

Run from the repository root:

```bash
pnpm dev:website
pnpm build:website
pnpm preview:website
```

The package-local equivalents are `pnpm --filter @mspace/website dev`, `build`, and `preview`.

## Deployment

Vercel deployment is configured by the root `vercel.json`:

- install command: `pnpm install --frozen-lockfile`
- build command: `pnpm --filter @mspace/website build`
- output directory: `apps/website/dist`

Deploy production from the repository root:

```bash
npx vercel@latest --prod
```

Do not commit `.vercel/`; it is a local project link and is ignored by git.

## Content Sources

- Brand mark: `apps/desktop/assets/brand/mspace-icon.png`; transparent website mark: `apps/website/src/assets/mspace-mark-transparent.png`
- Product screenshots: `docs/images/mspace-issues-list.png` and `docs/images/mspace-issue-detail.png`
- Changelog data: `apps/website/src/changelog.ts`
- Website backlog: `apps/website/TODO.md`

## Visual Guardrails

The website can be more provocative than the desktop app, but it should stay anti-generic and product-specific. Keep the copy anchored to concrete mspace objects: issues, sessions, worktrees, commits, namespaces, preview URLs, evidence, and cleanup state.

Do not turn the site into a generic AI-agent landing page. The desktop product remains a quiet Notion-like workspace; website styling should not leak into product routes.

Changelog entries should be public-facing product progress, not raw commit dumps or private operational notes. Group entries by calendar day and update the current day whenever a meaningful task ships.
