import type { IssueLabel, IssueLabelDefinition, UpdateIssueLabelsInput } from "@mspace/core";
import { cn } from "@mspace/ui";

const typeKeys = ["feat", "fix", "docs", "style", "refactor", "perf", "test", "build", "ci", "chore", "revert"];
const priorityKeys = ["p0", "p1", "p2", "p3"];

export type IssueLabelLike = Pick<IssueLabelDefinition | IssueLabel, "key" | "name" | "dimension" | "color">;
export type IssueLabelTone = "danger" | "warning" | "info" | "success" | "neutral" | "muted";

export interface IssueLabelPresentation {
  key: string;
  name: string;
  dimension: string;
  tone: IssueLabelTone;
  isPriority: boolean;
}

export const builtInIssueLabelOptions: IssueLabelDefinition[] = [
  ...typeKeys.map((name, index) => ({
    id: `type-${name}`,
    key: `type:${name}`,
    name,
    dimension: "type",
    color: "",
    sortOrder: index + 10,
    builtIn: true,
    createdAt: "",
    updatedAt: "",
  })),
  ...priorityKeys.map((name, index) => ({
    id: `priority-${name}`,
    key: `priority:${name}`,
    name: name.toUpperCase(),
    dimension: "priority",
    color: "",
    sortOrder: index + 10,
    builtIn: true,
    createdAt: "",
    updatedAt: "",
  })),
];

export function issueLabelOptionsForUI(definitions: IssueLabelDefinition[] | null | undefined) {
  if (!definitions || definitions.length === 0) return builtInIssueLabelOptions;
  const byKey = new Map(builtInIssueLabelOptions.map((label) => [label.key, label]));
  for (const label of definitions) {
    byKey.set(label.key, label);
  }
  return Array.from(byKey.values()).sort(compareIssueLabelDefinitions);
}

export function issueLabelOptionsByDimension(options: IssueLabelDefinition[], dimension: string) {
  return options.filter((label) => label.dimension === dimension);
}

export function selectedIssueLabelKey(labels: IssueLabel[], dimension: string) {
  return labels.map((label) => knownIssueLabelKey(label.key || label.name)).find((key) => issueLabelDimensionForKey(key) === dimension) || "";
}

export function issueLabelMatchesDimension(label: IssueLabel, dimension: string) {
  return label.dimension === dimension || issueLabelDimensionForKey(knownIssueLabelKey(label.key || label.name)) === dimension;
}

export function nextIssueLabelSelection(labels: IssueLabel[], dimension: string, nextKey: string) {
  const keys = labels
    .map((label) => knownIssueLabelKey(label.key || label.name))
    .filter((key) => key && issueLabelDimensionForKey(key) !== dimension);
  if (nextKey) keys.push(nextKey);
  return Array.from(new Set(keys));
}

export function buildIssueLabelSelectionInput(labelKeys: string[], options: IssueLabelDefinition[]): UpdateIssueLabelsInput {
  return {
    labelKeys,
    labels: labelKeys.map((key) => options.find((label) => label.key === key)?.name || key),
  };
}

export function issueLabelPresentation(label: IssueLabelLike): IssueLabelPresentation {
  const normalizedKey = normalizeIssueLabelKey(label.key || label.name);
  const knownKey = knownIssueLabelKey(normalizedKey);
  const key = knownKey || normalizedKey;
  const dimension = label.dimension || issueLabelDimensionForKey(knownKey) || issueLabelDimensionForKey(key);
  return {
    key,
    name: label.name || key,
    dimension,
    tone: issueLabelTone(key, dimension),
    isPriority: dimension === "priority",
  };
}

export function issueLabelBadgeClass(label: IssueLabelLike) {
  const presentation = issueLabelPresentation(label);
  return cn(
    "inline-flex h-5 max-w-full items-center gap-1 rounded-[6px] px-1.5 text-[11px] leading-4 shadow-[inset_0_0_0_1px_var(--line)]",
    presentation.isPriority ? "font-semibold" : "font-medium",
    presentation.isPriority ? softToneClass(presentation.tone) : "bg-[color:var(--block)] text-[color:var(--muted-strong)]",
  );
}

export function issueLabelDotClass(label: IssueLabelLike) {
  return cn("size-1.5 shrink-0 rounded-full", dotToneClass(issueLabelPresentation(label).tone));
}

export function issueLabelButtonClass(label: IssueLabelLike | null, active: boolean) {
  const tone = label ? issueLabelPresentation(label).tone : "neutral";
  return cn(
    "inline-flex min-h-8 items-center gap-1.5 rounded-[7px] px-2.5 text-[12px] font-medium leading-4 shadow-[inset_0_0_0_1px_var(--line)] transition-[background-color,color,box-shadow] duration-150 ease-out",
    active
      ? activeToneClass(tone)
      : label
        ? cn(softToneClass(tone), "hover:shadow-[inset_0_0_0_1px_var(--line)]")
        : "bg-transparent text-[color:var(--muted-strong)] hover:bg-[color:var(--hover)] hover:text-[color:var(--text)]",
  );
}

function issueLabelTone(key: string, dimension: string): IssueLabelTone {
  if (dimension === "priority") {
    if (key === "priority:p0") return "danger";
    if (key === "priority:p1") return "warning";
    if (key === "priority:p2") return "info";
    return "neutral";
  }

  if (key === "type:fix" || key === "type:revert") return "danger";
  if (key === "type:perf") return "warning";
  if (key === "type:feat" || key === "type:refactor") return "info";
  if (key === "type:docs" || key === "type:test") return "success";
  if (key === "type:chore") return "muted";
  return "neutral";
}

function softToneClass(tone: IssueLabelTone) {
  if (tone === "danger") return "bg-[color:var(--danger-soft)] text-[color:var(--danger)]";
  if (tone === "warning") return "bg-[color:var(--warning-soft)] text-[color:var(--warning)]";
  if (tone === "info") return "bg-[color:var(--blue-soft)] text-[color:var(--accent-blue)]";
  if (tone === "success") return "bg-[color:var(--success-soft)] text-[color:var(--success)]";
  if (tone === "muted") return "bg-[color:var(--block)] text-[color:var(--muted)]";
  return "bg-[color:var(--block)] text-[color:var(--muted-strong)]";
}

function dotToneClass(tone: IssueLabelTone) {
  if (tone === "danger") return "bg-[color:var(--danger)]";
  if (tone === "warning") return "bg-[color:var(--warning)]";
  if (tone === "info") return "bg-[color:var(--accent-blue)]";
  if (tone === "success") return "bg-[color:var(--success)]";
  if (tone === "muted") return "bg-[color:var(--faint)]";
  return "bg-[color:var(--muted-strong)]";
}

function activeToneClass(tone: IssueLabelTone) {
  if (tone === "danger") return "bg-[color:var(--danger)] text-[color:var(--paper)] shadow-[inset_0_0_0_1px_var(--danger)]";
  if (tone === "warning") return "bg-[color:var(--warning)] text-[color:var(--paper)] shadow-[inset_0_0_0_1px_var(--warning)]";
  if (tone === "info") return "bg-[color:var(--accent-blue)] text-[color:var(--paper)] shadow-[inset_0_0_0_1px_var(--accent-blue)]";
  if (tone === "success") return "bg-[color:var(--success)] text-[color:var(--paper)] shadow-[inset_0_0_0_1px_var(--success)]";
  if (tone === "muted") return "bg-[color:var(--muted-strong)] text-[color:var(--paper)] shadow-[inset_0_0_0_1px_var(--muted-strong)]";
  return "bg-[color:var(--text)] text-[color:var(--paper)] shadow-[inset_0_0_0_1px_var(--text)]";
}

function knownIssueLabelKey(value: string) {
  const normalized = normalizeIssueLabelKey(value);
  if (builtInIssueLabelOptions.some((label) => label.key === normalized)) return normalized;
  return "";
}

function normalizeIssueLabelKey(value: string) {
  const label = value.trim().replace(/^#/, "").trim().replace(/\s+/g, " ");
  if (!label) return "";
  const lower = label.toLowerCase();
  if (lower.includes(":")) {
    const [dimension, name] = lower.split(":", 2);
    return `${dimension.trim()}:${name.trim()}`;
  }
  if (typeKeys.includes(lower)) return `type:${lower}`;
  if (priorityKeys.includes(lower)) return `priority:${lower}`;
  return lower;
}

function issueLabelDimensionForKey(key: string) {
  return builtInIssueLabelOptions.find((label) => label.key === key)?.dimension || "";
}

function compareIssueLabelDefinitions(left: IssueLabelDefinition, right: IssueLabelDefinition) {
  const leftDimension = left.dimension === "type" ? 0 : left.dimension === "priority" ? 1 : 2;
  const rightDimension = right.dimension === "type" ? 0 : right.dimension === "priority" ? 1 : 2;
  if (leftDimension !== rightDimension) return leftDimension - rightDimension;
  if (left.sortOrder !== right.sortOrder) return left.sortOrder - right.sortOrder;
  return left.name.localeCompare(right.name);
}
