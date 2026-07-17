import { accessSync, constants, statSync } from "node:fs";
import { delimiter, join } from "node:path";

export type PersonalWorkerCapabilities = {
  protocolSmoke: boolean;
  codex: boolean;
  claudeCode: boolean;
  pi: boolean;
  dryRun: boolean;
  browser?: boolean;
  chrome_cdp?: boolean;
  [capability: string]: boolean | undefined;
};

type AgentExecutableDiscoveryOptions = {
  env?: NodeJS.ProcessEnv;
  homeDir: string;
  platform?: NodeJS.Platform;
  isExecutable?: (path: string) => boolean;
};

const AGENT_EXECUTABLES = {
  codex: "codex",
  claudeCode: "claude",
  pi: "pi",
} as const;

export type AgentEngineExecutable = {
  capability: keyof typeof AGENT_EXECUTABLES;
  command: string;
};

function readEnvironmentPath(env: NodeJS.ProcessEnv): string {
  const pathKey = Object.keys(env).find((key) => key.toLowerCase() === "path");
  return pathKey ? String(env[pathKey] || "") : "";
}

function stripPathQuotes(value: string): string {
  const trimmed = value.trim();
  return trimmed.length >= 2 && trimmed.startsWith('"') && trimmed.endsWith('"')
    ? trimmed.slice(1, -1)
    : trimmed;
}

function defaultExecutableDirectories(homeDir: string, env: NodeJS.ProcessEnv, platform: NodeJS.Platform): string[] {
  const configuredDirectories = [
    env.PNPM_HOME,
    env.BUN_INSTALL ? join(env.BUN_INSTALL, "bin") : undefined,
    env.VOLTA_HOME ? join(env.VOLTA_HOME, "bin") : undefined,
    env.NPM_CONFIG_PREFIX ? join(env.NPM_CONFIG_PREFIX, "bin") : undefined,
  ];
  if (platform === "win32") {
    return [
      ...configuredDirectories,
      env.APPDATA ? join(env.APPDATA, "npm") : undefined,
      join(homeDir, ".local", "bin"),
      join(homeDir, ".bun", "bin"),
    ].filter((value): value is string => Boolean(value));
  }
  return [
    ...configuredDirectories,
    join(homeDir, ".local", "bin"),
    join(homeDir, ".bun", "bin"),
    join(homeDir, "Library", "pnpm"),
    join(homeDir, ".npm-global", "bin"),
    ...(platform === "darwin" ? ["/opt/homebrew/bin"] : []),
    "/usr/local/bin",
    "/usr/bin",
  ].filter((value): value is string => Boolean(value));
}

export function buildAgentExecutableSearchPath(options: Omit<AgentExecutableDiscoveryOptions, "isExecutable">): string {
  const env = options.env || process.env;
  const platform = options.platform || process.platform;
  const separator = platform === "win32" ? ";" : delimiter;
  const directories = [
    ...readEnvironmentPath(env).split(separator),
    ...defaultExecutableDirectories(options.homeDir, env, platform),
  ];
  return [...new Set(directories.map(stripPathQuotes).filter(Boolean))].join(separator);
}

function isExecutableFile(path: string, platform: NodeJS.Platform): boolean {
  try {
    if (!statSync(path).isFile()) return false;
    if (platform !== "win32") accessSync(path, constants.X_OK);
    return true;
  } catch {
    return false;
  }
}

function executableFileNames(command: string, env: NodeJS.ProcessEnv, platform: NodeJS.Platform): string[] {
  if (platform !== "win32") return [command];
  const extensions = String(env.PATHEXT || ".COM;.EXE;.BAT;.CMD")
    .split(";")
    .map((extension) => extension.trim().toLowerCase())
    .filter(Boolean);
  return extensions.map((extension) => `${command}${extension}`);
}

export function discoverAgentEngineCapabilities(options: AgentExecutableDiscoveryOptions): Pick<PersonalWorkerCapabilities, "codex" | "claudeCode" | "pi"> {
  const env = options.env || process.env;
  const platform = options.platform || process.platform;
  const separator = platform === "win32" ? ";" : delimiter;
  const searchPath = buildAgentExecutableSearchPath({ env, homeDir: options.homeDir, platform });
  const directories = searchPath.split(separator).filter(Boolean);
  const canExecute = options.isExecutable || ((path: string) => isExecutableFile(path, platform));
  const hasExecutable = (command: string) => directories.some((directory) => (
    executableFileNames(command, env, platform).some((fileName) => canExecute(join(directory, fileName)))
  ));

  return {
    codex: hasExecutable(AGENT_EXECUTABLES.codex),
    claudeCode: hasExecutable(AGENT_EXECUTABLES.claudeCode),
    pi: hasExecutable(AGENT_EXECUTABLES.pi),
  };
}

export function personalWorkerRequiresCodexAuth(requiredCapabilities: Record<string, unknown> | undefined): boolean {
  return requiredCapabilities?.codex === true;
}

export function missingPersonalWorkerCapabilities(
  capabilities: Record<string, boolean | undefined>,
  requiredCapabilities: Record<string, unknown> | undefined,
): string[] {
  if (!requiredCapabilities) return [];
  return Object.entries(requiredCapabilities)
    .filter(([capability, required]) => required === true && capabilities[capability] !== true)
    .map(([capability]) => capability);
}

export function missingAgentEngineExecutables(
  capabilities: Pick<PersonalWorkerCapabilities, "codex" | "claudeCode" | "pi">,
  requiredCapabilities: Record<string, unknown> | undefined,
): AgentEngineExecutable[] {
  return missingPersonalWorkerCapabilities(capabilities, requiredCapabilities)
    .filter((capability): capability is keyof typeof AGENT_EXECUTABLES => capability in AGENT_EXECUTABLES)
    .map((capability) => ({ capability, command: AGENT_EXECUTABLES[capability] }));
}

export function personalWorkerRequiresBrowser(requiredCapabilities: Record<string, unknown> | undefined): boolean {
  return requiredCapabilities?.browser === true || requiredCapabilities?.chrome_cdp === true;
}

export function personalWorkerName(workspaceId: string, requiredCapabilities?: Record<string, unknown>): string {
  const baseName = `desktop-personal-${workspaceId.slice(0, 8)}`;
  return personalWorkerRequiresBrowser(requiredCapabilities) ? `${baseName}-browser` : baseName;
}

export function personalWorkerWorkRoot(baseRoot: string, requiredCapabilities?: Record<string, unknown>): string {
  return personalWorkerRequiresBrowser(requiredCapabilities) ? join(baseRoot, "browser-companion") : baseRoot;
}
