export type IssueWorkingCopyAvailabilityBlocker =
  | "working_copy_busy"
  | "working_copy_storage_unavailable"
  | "working_copy_recovery_required";

const issueWorkingCopyAvailabilityBlockers = new Set<IssueWorkingCopyAvailabilityBlocker>([
  "working_copy_busy",
  "working_copy_storage_unavailable",
  "working_copy_recovery_required",
]);

export function isIssueWorkingCopyAvailabilityBlocker(value: unknown): value is IssueWorkingCopyAvailabilityBlocker {
  return typeof value === "string" && issueWorkingCopyAvailabilityBlockers.has(value as IssueWorkingCopyAvailabilityBlocker);
}
