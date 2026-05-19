import { t } from "@mspace/i18n";

export const issueStatusOptions = [
  "open",
  "needs_review",
  "changes_requested",
  "ready_for_test",
  "blocked",
  "cancelled",
  "closed",
];

export function displayIssueStatus(status: string) {
  const normalized = status.trim().toLowerCase();
  if (normalized === "review" || normalized === "in_review") return "needs_review";
  if (normalized === "testing" || normalized === "test_in_progress") return "needs_review";
  if (normalized === "queued" || normalized === "running" || normalized === "in_progress") return "open";
  if (normalized === "test_passed" || normalized === "test_failed" || normalized === "failed") return "open";
  return normalized === "completed" || normalized === "done" ? "closed" : normalized;
}

export function issueStatusLabel(status: string) {
  const normalized = displayIssueStatus(status);
  if (normalized === "cancelled") return t("issueStatus.closed_as_not_planned");
  const translated = t(`issueStatus.${normalized}`, { defaultValue: "" });
  if (translated) return translated;
  return normalized.replace(/[_-]+/g, " ").replace(/\b\w/g, (letter) => letter.toUpperCase());
}
