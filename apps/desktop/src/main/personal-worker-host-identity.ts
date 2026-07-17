import { randomUUID } from "node:crypto";
import { mkdir, readFile, rename, rm, writeFile } from "node:fs/promises";
import { basename, dirname, join } from "node:path";

const HOST_ID_PATTERN = /^msh_[0-9a-f]{32}$/;
const HOST_IDENTITY_VERSION = 1;
const hostIdentityLoads = new Map<string, Promise<string>>();

type PersonalWorkerHostIdentity = {
  version: typeof HOST_IDENTITY_VERSION;
  hostId: string;
};

function parseHostIdentity(value: string): PersonalWorkerHostIdentity | null {
  try {
    const parsed = JSON.parse(value) as Partial<PersonalWorkerHostIdentity>;
    if (parsed.version !== HOST_IDENTITY_VERSION || !HOST_ID_PATTERN.test(String(parsed.hostId || ""))) {
      return null;
    }
    return {
      version: HOST_IDENTITY_VERSION,
      hostId: String(parsed.hostId),
    };
  } catch {
    return null;
  }
}

async function readHostIdentity(path: string): Promise<PersonalWorkerHostIdentity | null> {
  try {
    return parseHostIdentity(await readFile(path, "utf8"));
  } catch {
    return null;
  }
}

function createHostIdentity(): PersonalWorkerHostIdentity {
  return {
    version: HOST_IDENTITY_VERSION,
    hostId: `msh_${randomUUID().replaceAll("-", "")}`,
  };
}

export function personalWorkerHostIdentityPath(userDataPath: string): string {
  return join(userDataPath, "worker", "host-identity.json");
}

async function loadOrCreatePersonalWorkerHostIdUnlocked(identityPath: string): Promise<string> {
  const existing = await readHostIdentity(identityPath);
  if (existing) return existing.hostId;

  const identity = createHostIdentity();
  await mkdir(dirname(identityPath), { recursive: true });
  const tempPath = join(
    dirname(identityPath),
    `.${basename(identityPath)}.${process.pid}.${randomUUID()}.tmp`,
  );
  try {
    await writeFile(tempPath, `${JSON.stringify(identity, null, 2)}\n`, { mode: 0o600 });
    await rename(tempPath, identityPath);
  } finally {
    await rm(tempPath, { force: true }).catch(() => undefined);
  }
  return identity.hostId;
}

export async function loadOrCreatePersonalWorkerHostId(userDataPath: string): Promise<string> {
  const identityPath = personalWorkerHostIdentityPath(userDataPath);
  const current = hostIdentityLoads.get(identityPath);
  if (current) return current;

  const load = loadOrCreatePersonalWorkerHostIdUnlocked(identityPath);
  hostIdentityLoads.set(identityPath, load);
  try {
    return await load;
  } finally {
    if (hostIdentityLoads.get(identityPath) === load) {
      hostIdentityLoads.delete(identityPath);
    }
  }
}
