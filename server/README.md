# mspace Server

`server/` is the mspace control plane. It owns identity, workspaces, membership, auth sessions, and future GitHub App installation state.

The desktop app and local runner should become runtime clients of this service instead of owning collaboration identity themselves.

## Run

Create a local env file in the project root:

```bash
cp .env.example .env.local
```

Then edit `.env.local`:

```dotenv
DATABASE_URL=postgres://mspace:mspace@127.0.0.1:5432/mspace?sslmode=disable
MSPACE_GITHUB_CLIENT_ID=...
MSPACE_GITHUB_CLIENT_SECRET=...
MSPACE_GITHUB_REDIRECT_URI=http://127.0.0.1:8787/api/auth/github/callback
MSPACE_SERVER_ADDR=127.0.0.1:8787
```

Start the server:

```bash
pnpm run server
```

The server loads `.env`, `.env.local`, `server/.env`, and `server/.env.local` from the project root. Shell environment variables still take precedence over values from those files.

For local API-shape tests without Postgres, use the in-memory store from tests only. Production and shared development should use Postgres.

## Auth Shape

1. Desktop starts GitHub login through `GET /api/auth/github/start`.
2. Desktop opens the returned `authorizeUrl` in the browser.
3. GitHub redirects to `GET /api/auth/github/callback`.
4. The server validates OAuth state, exchanges the code with GitHub using the server-side client secret, upserts the mspace user, ensures a default workspace, issues an mspace session token, and stores a short-lived single-use login result for that OAuth state.
5. The callback renders a success page. It does not return raw auth JSON.
6. Desktop polls `GET /api/auth/github/result?state=...` and stores the returned `msp_...` token.
7. Desktop and runner clients call mspace APIs with `Authorization: Bearer <msp_...>`.

GitHub tokens are not the product session. They are used only to prove GitHub identity. Repository automation should later use GitHub App installation tokens owned by this service.

## API Slice

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/health` | Server health. |
| `GET` | `/api/auth/github/start` | Create OAuth state and return the GitHub authorization URL plus polling path. |
| `GET` | `/api/auth/github/callback` | Complete GitHub OAuth, link identity, create an mspace session, and render the browser success page. |
| `GET` | `/api/auth/github/result` | Poll the state-bound login result from the desktop app. Returns `202` while pending and consumes the result once ready. |
| `GET` | `/api/auth/me` | Return the current mspace user and workspaces for a bearer token. |
| `GET` | `/api/workspaces` | List the authenticated user's workspaces. |
