export function workspaceChangeStatusLabel(statusCode?: string) {
  const code = (statusCode || "").trim();

  if (!code) return "M";
  if (code === "??" || code.includes("A")) return "new";
  if (code === "!!") return "ignored";
  if (code.includes("U")) return "U";
  if (code.includes("R")) return "R";
  if (code.includes("C")) return "C";
  if (code.includes("D")) return "D";
  if (code.includes("M")) return "M";

  return code;
}

export function workspaceChangeStatusTone(statusCode?: string) {
  const label = workspaceChangeStatusLabel(statusCode);

  if (label === "new") return "text-[color:var(--success)]";
  if (label === "D" || label === "U") return "text-[color:var(--danger)]";
  if (label === "R" || label === "C") return "text-[color:var(--warning)]";
  return "text-[color:var(--accent-blue)]";
}

function stripLineSuffix(path: string) {
  return path.replace(/:\d+(?::\d+)?$/, "");
}

export function isWorkspaceDirectoryChange(change: { path?: string }) {
  return /[\\/]$/.test(stripLineSuffix(change.path || "").trim());
}

export function visibleWorkspaceFileChanges<T extends { path?: string }>(changes: T[]) {
  return changes.filter((change) => !isWorkspaceDirectoryChange(change));
}
