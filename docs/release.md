# mspace Release Process

mspace uses GitHub Releases for project-level releases. A release is a repository milestone for mspace itself, not an Issue or PR handoff inside the product.

## Version Model

- Use one repository version for the desktop app, website, server, worker, and shared packages.
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

Release titles should start with the tag and should not repeat the repository name. Use titles like:

```text
v0.1.0: Personal Workspace MVP
v0.2.0: Team Workspace MVP
v0.2.1: Team Runtime fixes
```

The public website changelog in `apps/website/src/changelog.ts` is a progress log, not the source of truth for release notes. Keep both current when a release ships meaningful product, engineering, documentation, or website progress.

## Manual Tag Gate

The release decision stays manual. Create an annotated tag only after choosing the exact commit that should represent the version.

```bash
git fetch origin --tags
git show --no-patch --pretty=fuller <commit>
git tag -a v0.1.0 <commit> -m "v0.1.0"
git push origin v0.1.0
```

After the tag is pushed, GitHub Actions creates or updates a draft GitHub Release, attaches the release verification summary, and uploads macOS, Windows, and Linux desktop artifacts. The same workflow also has a manual `workflow_dispatch` input named `tag` for rerunning desktop packaging on an existing tag that already contains the complete trustworthy packaging contract.

The tagged tree is the only release source. The workflow verifies `refs/tags/<tag>`, requires the tag commit to equal checkout `HEAD`, requires a clean checkout, and requires `v<package.json version>`. It never copies resolver, prepare, builder, or other packaging files from the workflow commit into older tagged source. If a tag lacks the current packaging contract, create a patched point release from a commit that contains the complete toolchain.

## Validation

The release workflow should verify the tagged commit with:

```bash
pnpm install --frozen-lockfile
pnpm typecheck
pnpm --filter @mspace/desktop build
pnpm --filter @mspace/desktop dist:mac
pnpm --filter @mspace/desktop dist:win
pnpm --filter @mspace/desktop dist:linux
pnpm --filter @mspace/website build
(cd server && go test ./...)
(cd server && go build ./cmd/server)
(cd worker && go test ./...)
(cd worker && go build ./...)
```

The workflow does not deploy production services, write databases, publish container images, or create npm packages.

## Desktop Artifacts

Desktop releases are built with `electron-builder` from `apps/desktop`.

The desktop package includes:

- the Electron renderer/main/preload build from `electron-vite`;
- bundled `mspace-server` and `mspace-worker` binaries under app resources;
- a local personal SQLite store at the app user-data path when no remote server is configured;
- macOS `.dmg` and `.zip` artifacts for `arm64` and `x64`;
- Windows `.exe` and `.zip` artifacts for `x64`;
- Linux `.AppImage`, `.deb`, and `.rpm` artifacts for `x64`.

The packaged app chooses its server in this order:

1. `MSPACE_SERVER_URL`, when set, locks the desktop app to that remote server for the launch.
2. A user-configured server URL in the desktop settings connects to a remote team/customer server.
3. The default personal mode starts the bundled local server with `MSPACE_STORE=sqlite` and `MSPACE_SQLITE_PATH=<userData>/mspace.db`.

Build local desktop packages with:

```bash
pnpm dist:desktop:mac
pnpm dist:desktop:win
pnpm dist:desktop:linux
```

Local packaging binds `MSPACE_BUILD_VERSION` to the root package version and `MSPACE_BUILD_COMMIT_SHA` to checkout `HEAD`, then rejects dirty desktop, shared-package, Server, or Worker build inputs before replacing bundled binaries. Commit the intended source first; do not use identity environment variables to label uncommitted or different source as a release build.

Unsigned artifacts are acceptable for internal dogfood. Public customer downloads should add Apple Developer ID signing and notarization before being marked ready.

Tags earlier than trustworthy packaged build identity, including `v0.1.0` and tags whose Worker version cannot be injected, should not be backfilled by attaching newly generated installers to the old release. Create a patched point release from a commit that contains the complete packaging and identity support instead.

## Release Channels

- Stable tags: `v0.1.0`, `v0.2.0`, `v0.2.1`
- Candidate tags: `v0.2.0-rc.1`

Stable releases are for known-good milestones. Candidate releases are for dogfood points that need validation before they become a stable release.
