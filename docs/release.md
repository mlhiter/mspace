# mspace Release Process

mspace uses GitHub Releases for project-level releases. A release is a repository milestone for mspace itself, not an Issue or PR handoff inside the product.

## Version Model

- Use one repository version for the desktop app, website, server, runner, worker, and shared packages.
- Use annotated tags named `vMAJOR.MINOR.PATCH`.
- Use `v0.x.0` for user-visible milestones and `v0.x.y` for focused fixes to the latest milestone.
- Use `v0.x.0-rc.N` only when a release candidate needs dogfood before becoming the stable tag.
- Keep `main` as the active dogfood branch. A tag is the release boundary.

## First Release Boundary

`v0.1.0` is the Personal Workspace MVP release. It is intentionally tagged at:

```text
7285632c099627686318f27dd4c642f5747c5213
```

This is the latest personal-workspace point before the team collaboration and Team Runtime work started. Team collaboration code on later `main` commits is intentionally excluded from `v0.1.0` because it is still unstable.

## Release Notes

GitHub Release notes should describe the release from a user and operator point of view:

- what became usable;
- what changed since the previous release;
- what is intentionally excluded;
- known limitations;
- the exact tag and commit.

The public website changelog in `apps/website/src/changelog.ts` is a progress log, not the source of truth for release notes. Keep both current when a release ships meaningful product, engineering, documentation, or website progress.

## Manual Tag Gate

The release decision stays manual. Create an annotated tag only after choosing the exact commit that should represent the version.

```bash
git fetch origin --tags
git show --no-patch --pretty=fuller <commit>
git tag -a v0.1.0 <commit> -m "v0.1.0"
git push origin v0.1.0
```

After the tag is pushed, GitHub Actions creates or updates a draft GitHub Release and attaches the release verification summary.

## Validation

The release workflow should verify the tagged commit with:

```bash
pnpm install --frozen-lockfile
pnpm typecheck
pnpm --filter @mspace/desktop build
pnpm --filter @mspace/website build
(cd server && go test ./...)
(cd server && go build ./cmd/server)
(cd runner && go test ./...)
(cd runner && go build ./...)
(cd worker && go test ./...)
(cd worker && go build ./...)
```

The workflow does not deploy production services, write databases, publish container images, or create npm packages.

## Release Channels

- Stable tags: `v0.1.0`, `v0.2.0`, `v0.2.1`
- Candidate tags: `v0.2.0-rc.1`

Stable releases are for known-good milestones. Candidate releases are for dogfood points that need validation before they become a stable release.
