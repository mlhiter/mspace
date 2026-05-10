import type { IssueLabelLike } from "./issue-labels";
import { issueLabelBadgeClass, issueLabelDotClass, issueLabelPresentation } from "./issue-labels";
import { cn } from "@mspace/ui";

export function IssueLabelBadge(props: {
  label: IssueLabelLike;
  className?: string;
  prefix?: string;
}) {
  const presentation = issueLabelPresentation(props.label);

  return (
    <span className={cn(issueLabelBadgeClass(props.label), props.className)} title={presentation.name}>
      {props.prefix ? <span className="shrink-0 text-[color:var(--faint)]">{props.prefix}</span> : null}
      {presentation.isPriority ? null : <span className={issueLabelDotClass(props.label)} />}
      <span className="truncate">{presentation.name}</span>
    </span>
  );
}

export function IssueLabelOptionLabel(props: {
  label: IssueLabelLike;
}) {
  const presentation = issueLabelPresentation(props.label);

  return (
    <span className="flex min-w-0 items-center gap-2">
      <span className={issueLabelDotClass(props.label)} />
      <span className="truncate">{presentation.name}</span>
    </span>
  );
}

export function IssueLabelSelectValue(props: {
  label?: IssueLabelLike;
  fallback: string;
}) {
  if (!props.label) {
    return <span className="truncate text-[color:var(--faint)]">{props.fallback}</span>;
  }

  return <IssueLabelBadge label={props.label} className="min-w-0" />;
}
