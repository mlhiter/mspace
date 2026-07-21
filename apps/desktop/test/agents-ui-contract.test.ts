import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";
import { agentAvatarUrl } from "../../../packages/views/src/agent-avatar.ts";

const repoRoot = new URL("../../../", import.meta.url);

test("fixed Agent identities resolve to their own bundled assets", () => {
  assert.match(agentAvatarUrl("codex"), /^data:image\/png;base64,/);
  assert.equal(new URL(agentAvatarUrl("claude_code")).pathname.endsWith("/assets/claude.svg"), true);
  assert.equal(new URL(agentAvatarUrl("pi")).pathname.endsWith("/assets/pi.svg"), true);
});

test("Agents readiness surfaces do not restore fixed-width overflow layouts", async () => {
  const source = await readFile(new URL("packages/views/src/agents-page.tsx", repoRoot), "utf8");

  assert.doesNotMatch(source, /min-w-\[(?:780|1120)px\]/);
  assert.match(source, /md:grid-cols-\[minmax\(0,1\.4fr\)_minmax\(0,0\.72fr\)_minmax\(0,0\.85fr\)\]/);
  assert.match(source, /lg:grid-cols-3/);
});
