import { getMspaceLocale, t, useMspaceTranslation } from "@mspace/i18n";

export function formatAbsoluteTime(value: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value || t("common.unknown");
  return date.toLocaleString(getMspaceLocale());
}

export function formatRelativeTime(value: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value || t("common.unknown");

  const diffMs = date.getTime() - Date.now();
  const absMs = Math.abs(diffMs);
  const locale = getMspaceLocale();
  const rtf = new Intl.RelativeTimeFormat(locale, { numeric: "auto" });
  const minute = 60_000;
  const hour = 60 * minute;
  const day = 24 * hour;

  if (absMs < 45_000) return t("time.justNow");
  if (absMs < 45 * minute) return rtf.format(Math.round(diffMs / minute), "minute");
  if (absMs < 22 * hour) return rtf.format(Math.round(diffMs / hour), "hour");
  if (absMs < 6 * day) return rtf.format(Math.round(diffMs / day), "day");

  const sameYear = date.getFullYear() === new Date().getFullYear();
  const formatted = new Intl.DateTimeFormat(locale, {
    month: "short",
    day: "numeric",
    ...(sameYear ? {} : { year: "numeric" }),
  }).format(date);
  return t("time.onDate", { date: formatted });
}

export function RelativeTime(props: { value: string; prefix?: string }) {
  useMspaceTranslation();

  return (
    <span title={formatAbsoluteTime(props.value)}>
      {props.prefix ? `${props.prefix} ` : ""}
      {formatRelativeTime(props.value)}
    </span>
  );
}
