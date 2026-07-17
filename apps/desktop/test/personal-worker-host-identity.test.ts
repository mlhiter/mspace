import assert from "node:assert/strict";
import { mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import {
  loadOrCreatePersonalWorkerHostId,
  personalWorkerHostIdentityPath,
} from "../src/main/personal-worker-host-identity.ts";

async function withUserData(run: (userDataPath: string) => Promise<void>): Promise<void> {
  const userDataPath = await mkdtemp(join(tmpdir(), "mspace-host-identity-"));
  try {
    await run(userDataPath);
  } finally {
    await rm(userDataPath, { recursive: true, force: true });
  }
}

test("creates and stably reuses an anonymous personal worker host identity", async () => {
  await withUserData(async (userDataPath) => {
    const first = await loadOrCreatePersonalWorkerHostId(userDataPath);
    const second = await loadOrCreatePersonalWorkerHostId(userDataPath);
    assert.equal(second, first);
    assert.match(first, /^msh_[0-9a-f]{32}$/);

    const persisted = JSON.parse(await readFile(personalWorkerHostIdentityPath(userDataPath), "utf8"));
    assert.deepEqual(persisted, { version: 1, hostId: first });
  });
});

test("concurrent identity loads resolve to one persisted host identity", async () => {
  await withUserData(async (userDataPath) => {
    const hostIds = await Promise.all(
      Array.from({ length: 12 }, () => loadOrCreatePersonalWorkerHostId(userDataPath)),
    );
    assert.equal(new Set(hostIds).size, 1);
    const persisted = JSON.parse(await readFile(personalWorkerHostIdentityPath(userDataPath), "utf8"));
    assert.equal(persisted.hostId, hostIds[0]);
  });
});

test("regenerates a safe identity when the persisted file is corrupt", async () => {
  await withUserData(async (userDataPath) => {
    const identityPath = personalWorkerHostIdentityPath(userDataPath);
    await mkdir(join(userDataPath, "worker"), { recursive: true });
    await writeFile(identityPath, JSON.stringify({ version: 1, hostId: "hostname-and-username" }));

    const regenerated = await loadOrCreatePersonalWorkerHostId(userDataPath);
    assert.match(regenerated, /^msh_[0-9a-f]{32}$/);
    assert.notEqual(regenerated, "hostname-and-username");
    const persisted = JSON.parse(await readFile(identityPath, "utf8"));
    assert.deepEqual(persisted, { version: 1, hostId: regenerated });
  });
});
