export const issueStatusOptions = [
  "open",
  "in_progress",
  "needs_review",
  "changes_requested",
  "ready_for_test",
  "test_in_progress",
  "test_passed",
  "test_failed",
  "blocked",
  "failed",
  "cancelled",
  "closed",
];

const issueStatusLabels: Record<string, string> = {
  open: "Open",
  in_progress: "In progress",
  needs_review: "Needs review",
  changes_requested: "Changes requested",
  ready_for_test: "Ready for test",
  test_in_progress: "Test in progress",
  test_passed: "Test passed",
  test_failed: "Test failed",
  blocked: "Blocked",
  failed: "Failed",
  cancelled: "Cancelled",
  closed: "Closed",
};

export function displayIssueStatus(status: string) {
  const normalized = status.trim().toLowerCase();
  if (normalized === "review" || normalized === "in_review") return "needs_review";
  if (normalized === "testing") return "test_in_progress";
  if (normalized === "queued" || normalized === "running") return "in_progress";
  return normalized === "completed" || normalized === "done" ? "closed" : normalized;
}

export function issueStatusLabel(status: string) {
  const normalized = displayIssueStatus(status);
  if (issueStatusLabels[normalized]) return issueStatusLabels[normalized];
  return normalized.replace(/[_-]+/g, " ").replace(/\b\w/g, (letter) => letter.toUpperCase());
}
