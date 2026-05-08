export function formatAbsoluteTime(value: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value || "unknown";
  return date.toLocaleString();
}

export function formatRelativeTime(value: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value || "unknown";

  const diffMs = date.getTime() - Date.now();
  const absMs = Math.abs(diffMs);
  const rtf = new Intl.RelativeTimeFormat("en", { numeric: "auto" });
  const minute = 60_000;
  const hour = 60 * minute;
  const day = 24 * hour;

  if (absMs < 45_000) return "just now";
  if (absMs < 45 * minute) return rtf.format(Math.round(diffMs / minute), "minute");
  if (absMs < 22 * hour) return rtf.format(Math.round(diffMs / hour), "hour");
  if (absMs < 6 * day) return rtf.format(Math.round(diffMs / day), "day");

  const sameYear = date.getFullYear() === new Date().getFullYear();
  return `on ${new Intl.DateTimeFormat("en", {
    month: "short",
    day: "numeric",
    ...(sameYear ? {} : { year: "numeric" }),
  }).format(date)}`;
}

export function RelativeTime(props: { value: string; prefix?: string }) {
  return (
    <span title={formatAbsoluteTime(props.value)}>
      {props.prefix ? `${props.prefix} ` : ""}
      {formatRelativeTime(props.value)}
    </span>
  );
}
