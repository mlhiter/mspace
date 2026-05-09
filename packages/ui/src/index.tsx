import {
  Archive,
  ArrowLeft,
  ArrowRight,
  Bot,
  Boxes,
  CheckCircle2,
  ChevronRight,
  Circle,
  CircleAlert,
  Clock3,
  Files,
  FolderKanban,
  Inbox,
  Layers3,
  LoaderCircle,
  type LucideIcon,
  MessageSquarePlus,
  MessageSquareText,
  Plus,
  Search,
  Sparkles,
  SquareTerminal,
} from "lucide-react";
import { useEffect, useState, type ComponentProps, type PropsWithChildren, type ReactNode } from "react";
import {
  Link,
  Outlet,
  type HistoryLocation,
  useCanGoBack,
  useRouter,
  useRouterState,
} from "@tanstack/react-router";
import { Alert, AlertDescription } from "./components/ui/alert";
import { Badge } from "./components/ui/badge";
import {
  Button as ShadcnButton,
  type buttonVariants as shadcnButtonVariants,
} from "./components/ui/button";
import { Card, CardAction, CardContent, CardHeader, CardTitle } from "./components/ui/card";
import {
  Field as ShadcnField,
  FieldDescription,
  FieldLabel,
} from "./components/ui/field";
import { Input as ShadcnInput } from "./components/ui/input";
import { ScrollArea } from "./components/ui/scroll-area";
import { Textarea as ShadcnTextarea } from "./components/ui/textarea";
import { cn } from "./lib/utils";

export { cn } from "./lib/utils";
export {
  Alert,
  AlertAction,
  AlertDescription,
  AlertTitle,
} from "./components/ui/alert";
export { Badge, badgeVariants } from "./components/ui/badge";
export { Button as ShadcnButton, buttonVariants } from "./components/ui/button";
export {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from "./components/ui/card";
export {
  FieldContent,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
  FieldLegend,
  FieldSeparator,
  FieldSet,
  FieldTitle,
} from "./components/ui/field";
export { Input as ShadcnInput } from "./components/ui/input";
export { Label } from "./components/ui/label";
export {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectLabel,
  SelectScrollDownButton,
  SelectScrollUpButton,
  SelectSeparator,
  SelectTrigger,
  SelectValue,
} from "./components/ui/select";
export { ScrollArea, ScrollBar } from "./components/ui/scroll-area";
export { Separator } from "./components/ui/separator";
export { Textarea as ShadcnTextarea } from "./components/ui/textarea";

const sidebarItems = [
  { to: "/inbox", label: "Inbox", icon: Inbox },
  { to: "/issues", label: "Issues", icon: MessageSquareText },
  { to: "/agents", label: "Agents", icon: Bot },
  { to: "/projects", label: "Projects", icon: FolderKanban },
];

const sidebarProjects = [
  { name: "mspace", namespace: "mspace-dev", state: "active" },
  { name: "kite", namespace: "kite-system", state: "watch" },
  { name: "devbox", namespace: "devbox-70", state: "idle" },
];

let maxObservedHistoryIndex = 0;

function rememberHistoryIndex(index: number, actionType?: string) {
  maxObservedHistoryIndex = actionType === "PUSH" ? index : Math.max(maxObservedHistoryIndex, index);
  return maxObservedHistoryIndex;
}

export type BreadcrumbItem = {
  label: string;
  to?: string;
  params?: Record<string, string>;
  search?: Record<string, unknown>;
};

export function AppShell(props: { brandLogoSrc?: string } = {}) {
  return (
    <div className="grid h-full min-h-0 grid-cols-[252px_minmax(0,1fr)] bg-[color:var(--canvas)] text-[color:var(--text)]">
      <div className="app-titlebar" aria-hidden="true" />
      <aside className="flex min-h-0 flex-col border-r border-[color:var(--line)] bg-[color:var(--sidebar)] px-3 pb-4 pt-12">
        <div className="mb-4 flex items-center px-2">
          <div className="flex min-w-0 items-center gap-2.5">
            <div className="grid size-8 shrink-0 place-items-center rounded-[8px] bg-[color:var(--paper)] text-[color:var(--ink)] shadow-[inset_0_0_0_1px_var(--line)]">
              {props.brandLogoSrc ? (
                <img src={props.brandLogoSrc} alt="" className="h-5 w-7 object-contain" />
              ) : (
                <span className="text-[13px] font-semibold">m</span>
              )}
            </div>
            <div className="min-w-0">
              <div className="truncate text-[13px] font-semibold leading-5">mspace</div>
              <div className="truncate text-[11px] leading-4 text-[color:var(--muted)]">K8s agent workspace</div>
            </div>
          </div>
        </div>

        <div className="mb-2 rounded-[10px] bg-[color:var(--paper)] px-2.5 py-2 shadow-[inset_0_0_0_1px_var(--line)]">
          <div className="flex items-center gap-2 text-[12px] text-[color:var(--muted)]">
            <Search data-icon />
            <span>Search projects, sessions, evidence</span>
          </div>
        </div>
        <SidebarActionLink to="/issues" search={{ new: "1" }} icon={MessageSquarePlus}>
          New issue
        </SidebarActionLink>

        <nav className="flex flex-col gap-1">
          {sidebarItems.map((item) => (
            <SidebarLink key={item.to} to={item.to} icon={item.icon}>
              {item.label}
            </SidebarLink>
          ))}
        </nav>

        <div className="mt-5 flex items-center justify-between px-2 text-[12px] font-medium text-[color:var(--faint)]">
          <span>Namespaces</span>
          <Boxes data-icon />
        </div>
        <div className="mt-2 flex flex-col gap-1">
          {sidebarProjects.map((project) => (
            <button
              key={project.namespace}
              type="button"
              className="group flex w-full items-center gap-2 rounded-[7px] px-2 py-1.5 text-left text-[13px] transition-[background-color,transform] duration-150 ease-out hover:bg-[color:var(--hover)] active:scale-[0.98]"
            >
              <span
                className={cn(
                  "size-2 rounded-full",
                  project.state === "active" && "bg-[color:var(--muted-strong)]",
                  project.state === "watch" && "bg-[color:var(--muted)]",
                  project.state === "idle" && "bg-[color:var(--faint)]",
                )}
              />
              <span className="min-w-0 flex-1">
                <span className="block truncate font-medium">{project.name}</span>
                <span className="block truncate text-[11px] text-[color:var(--muted)]">{project.namespace}</span>
              </span>
              <ChevronRight
                data-icon
                className="opacity-0 transition-[opacity,transform] duration-150 ease-out group-hover:opacity-100"
              />
            </button>
          ))}
        </div>

        <div className="mt-auto rounded-[10px] bg-[color:var(--paper)] px-3 py-3 shadow-[inset_0_0_0_1px_var(--line)]">
          <div className="flex items-center gap-2 text-[12px] font-medium">
            <SquareTerminal data-icon className="text-[color:var(--muted)]" />
            Local runner
          </div>
          <p className="mt-1.5 text-[12px] leading-5 text-[color:var(--muted)]">
            Edits stay local. Kubernetes evidence stays attached to the issue.
          </p>
        </div>
      </aside>
      <main className="min-h-0 min-w-0 bg-[color:var(--paper)]">
        <ScrollArea className="h-full">
          <div className="min-h-full pt-7">
            <Outlet />
          </div>
        </ScrollArea>
      </main>
    </div>
  );
}

export function SidebarLink(props: PropsWithChildren<{ to: string; icon: LucideIcon }>) {
  const Icon = props.icon;
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  const isActive = pathname === props.to || pathname.startsWith(`${props.to}/`);

  return (
    <Link
      to={props.to}
      className={cn(
        "group flex items-center gap-2 rounded-[7px] px-2 py-1.5 text-[13px] font-medium transition-[background-color,color,transform] duration-150 ease-out active:scale-[0.98]",
        isActive
          ? "bg-[color:var(--selection)] text-[color:var(--text)]"
          : "text-[color:var(--muted)] hover:bg-[color:var(--hover)] hover:text-[color:var(--text)]",
      )}
    >
      <Icon data-icon />
      <span>{props.children}</span>
    </Link>
  );
}

export function SidebarActionLink(props: PropsWithChildren<{ to: string; search?: Record<string, unknown>; icon: LucideIcon }>) {
  const Icon = props.icon;
  return (
    <Link
      to={props.to}
      search={props.search}
      className="mb-3 group flex items-center gap-2 rounded-[7px] px-2 py-1.5 text-[13px] font-medium text-[color:var(--muted)] transition-[background-color,color,transform] duration-150 ease-out hover:bg-[color:var(--hover)] hover:text-[color:var(--text)] active:scale-[0.98]"
    >
      <Icon data-icon />
      <span>{props.children}</span>
    </Link>
  );
}

function NavigationControls() {
  const router = useRouter();
  const canGoBack = useCanGoBack();
  const historyIndex = useRouterState({ select: (state) => state.location.state.__TSR_index });
  const [maxHistoryIndex, setMaxHistoryIndex] = useState(() => rememberHistoryIndex(historyIndex));
  const canGoForward = historyIndex < maxHistoryIndex;

  useEffect(() => {
    setMaxHistoryIndex(rememberHistoryIndex(historyIndex));
  }, [historyIndex]);

  useEffect(() => {
    return router.history.subscribe(({ location, action }: { location: HistoryLocation; action: { type: string } }) => {
      setMaxHistoryIndex(rememberHistoryIndex(location.state.__TSR_index, action.type));
    });
  }, [router]);

  return (
    <div className="flex shrink-0 items-center gap-1">
      <button
        type="button"
        aria-label="Go back"
        disabled={!canGoBack}
        className="grid size-7 place-items-center rounded-[7px] text-[color:var(--muted)] transition-[background-color,color,transform,opacity] duration-150 ease-out hover:bg-[color:var(--hover)] hover:text-[color:var(--text)] active:scale-95 disabled:pointer-events-none disabled:opacity-35"
        onClick={() => router.history.back()}
      >
        <ArrowLeft data-icon />
      </button>
      <button
        type="button"
        aria-label="Go forward"
        disabled={!canGoForward}
        className="grid size-7 place-items-center rounded-[7px] text-[color:var(--muted)] transition-[background-color,color,transform,opacity] duration-150 ease-out hover:bg-[color:var(--hover)] hover:text-[color:var(--text)] active:scale-95 disabled:pointer-events-none disabled:opacity-35"
        onClick={() => router.history.forward()}
      >
        <ArrowRight data-icon />
      </button>
    </div>
  );
}

function BreadcrumbTrail(props: { items: BreadcrumbItem[] }) {
  return (
    <nav aria-label="Breadcrumb" className="min-w-0 flex-1 overflow-hidden">
      <ol className="flex min-w-0 items-center gap-1.5 text-[12px] text-[color:var(--muted)]">
        {props.items.map((item, index) => {
          const isLast = index === props.items.length - 1;
          const content = (
            <>
              {index === 0 ? <Layers3 data-icon className="shrink-0" /> : null}
              <span className="truncate">{item.label}</span>
            </>
          );

          return (
            <li key={`${index}-${item.label}`} className="flex min-w-0 items-center gap-1.5">
              {index > 0 ? <ChevronRight data-icon className="shrink-0 text-[color:var(--faint)]" /> : null}
              {item.to && !isLast ? (
                <Link
                  to={item.to}
                  params={item.params}
                  search={item.search}
                  className="flex min-w-0 items-center gap-1.5 rounded-[6px] px-1 py-0.5 transition-[background-color,color] duration-150 ease-out hover:bg-[color:var(--hover)] hover:text-[color:var(--text)]"
                >
                  {content}
                </Link>
              ) : (
                <span className={cn("flex min-w-0 items-center gap-1.5 px-1 py-0.5", isLast && "text-[color:var(--muted-strong)]")}>
                  {content}
                </span>
              )}
            </li>
          );
        })}
      </ol>
    </nav>
  );
}

export function PageFrame(props: PropsWithChildren<{ title: string; subtitle?: string; actions?: ReactNode; breadcrumbs?: BreadcrumbItem[] }>) {
  const breadcrumbs = props.breadcrumbs || [
    { label: "mspace", to: "/inbox" },
    { label: props.title },
  ];

  return (
    <div className="mx-auto min-h-full w-full max-w-[1280px] px-10 pb-12 pt-8">
      <div className="mb-8 flex items-start justify-between gap-6 border-b border-[color:var(--line)] pb-6">
        <div className="min-w-0">
          <div className="mb-3 flex min-w-0 items-center gap-2">
            <NavigationControls />
            <BreadcrumbTrail items={breadcrumbs} />
          </div>
          <h1 className="page-title text-[32px] font-semibold leading-[1.1] text-[color:var(--text)]">
            {props.title}
          </h1>
          {props.subtitle ? (
            <p className="mt-3 max-w-[72ch] text-[14px] leading-6 text-[color:var(--muted)] text-pretty">
              {props.subtitle}
            </p>
          ) : null}
        </div>
        {props.actions ? <div className="flex shrink-0 flex-wrap items-center justify-end gap-2">{props.actions}</div> : null}
      </div>
      {props.children}
    </div>
  );
}

export function Panel(props: PropsWithChildren<{ title?: string; aside?: ReactNode; className?: string }>) {
  return (
    <Card
      size="sm"
      className={cn(
        "gap-0 rounded-[10px] bg-[color:var(--surface)] py-4 text-[color:var(--text)] shadow-[inset_0_0_0_1px_var(--line)] ring-0",
        props.className,
      )}
    >
      {props.title || props.aside ? (
        <CardHeader className="mb-3 flex min-h-7 grid-cols-[1fr_auto] items-center gap-4 px-4">
          {props.title ? (
            <CardTitle className="text-[13px] font-semibold leading-5 text-[color:var(--muted-strong)]">
              {props.title}
            </CardTitle>
          ) : (
            <span />
          )}
          {props.aside ? <CardAction>{props.aside}</CardAction> : null}
        </CardHeader>
      ) : null}
      <CardContent className="px-4">{props.children}</CardContent>
    </Card>
  );
}

type ShadcnButtonProps = ComponentProps<typeof ShadcnButton>;
type ShadcnButtonVariant = NonNullable<Parameters<typeof shadcnButtonVariants>[0]>["variant"];
type ShadcnButtonSize = NonNullable<Parameters<typeof shadcnButtonVariants>[0]>["size"];

export interface ButtonProps extends Omit<ShadcnButtonProps, "variant" | "size"> {
  variant?: "primary" | "secondary" | "ghost" | "danger";
  size?: "sm" | "md" | "lg" | "icon";
  asChild?: boolean;
}

export function Button({ className, variant, size, asChild = false, ...props }: ButtonProps) {
  const shadcnVariant: ShadcnButtonVariant =
    variant === "danger"
      ? "destructive"
      : variant === "secondary"
        ? "outline"
        : variant === "ghost"
          ? "ghost"
          : "default";
  const shadcnSize: ShadcnButtonSize = size === "md" ? "lg" : size || "lg";

  return (
    <ShadcnButton
      asChild={asChild}
      variant={shadcnVariant}
      size={shadcnSize}
      className={cn(
        "min-h-9 rounded-[7px] text-[13px] transition-[background-color,color,box-shadow,transform,opacity] duration-150 ease-out active:scale-95 [&_[data-icon]]:size-4",
        variant === "primary" || !variant
          ? "bg-[color:var(--ink)] text-[color:var(--paper)] hover:bg-[color:var(--ink-soft)]"
          : null,
        variant === "secondary"
          ? "bg-[color:var(--surface)] text-[color:var(--text)] shadow-[0_0_0_1px_var(--line)] hover:bg-[color:var(--hover)]"
          : null,
        variant === "danger" ? "bg-[color:var(--danger)] text-white hover:bg-[color:var(--danger-strong)]" : null,
        className,
      )}
      {...props}
    />
  );
}

export function Input({ className, ...props }: ComponentProps<typeof ShadcnInput>) {
  return (
    <ShadcnInput
      className={cn(
        "min-h-9 rounded-[7px] border-0 bg-[color:var(--surface)] px-3 text-[13px] text-[color:var(--text)] shadow-[0_0_0_1px_var(--line)] transition-[background-color,box-shadow] duration-150 ease-out placeholder:text-[color:var(--faint)] focus-visible:bg-[color:var(--paper)] focus-visible:ring-0 focus-visible:shadow-[0_0_0_1px_var(--accent),0_0_0_3px_var(--accent-soft)]",
        className,
      )}
      {...props}
    />
  );
}

export function Textarea({ className, ...props }: ComponentProps<typeof ShadcnTextarea>) {
  return (
    <ShadcnTextarea
      className={cn(
        "min-h-28 resize-y rounded-[7px] border-0 bg-[color:var(--surface)] px-3 py-2 text-[13px] leading-6 text-[color:var(--text)] shadow-[0_0_0_1px_var(--line)] transition-[background-color,box-shadow] duration-150 ease-out placeholder:text-[color:var(--faint)] focus-visible:bg-[color:var(--paper)] focus-visible:ring-0 focus-visible:shadow-[0_0_0_1px_var(--accent),0_0_0_3px_var(--accent-soft)]",
        className,
      )}
      {...props}
    />
  );
}

export function Field(props: PropsWithChildren<{ label: string; hint?: string }>) {
  return (
    <ShadcnField className="flex flex-col gap-1.5">
      <FieldLabel className="text-[13px] font-medium leading-5 text-[color:var(--muted-strong)]">
        {props.label}
      </FieldLabel>
      {props.children}
      {props.hint ? (
        <FieldDescription className="text-[12px] leading-5 text-[color:var(--muted)]">
          {props.hint}
        </FieldDescription>
      ) : null}
    </ShadcnField>
  );
}

export function StatusBadge(props: { value: string }) {
  const tone =
    props.value === "completed"
      ? "bg-[color:var(--success-soft)] text-[color:var(--success)]"
      : props.value === "failed"
        ? "bg-[color:var(--danger-soft)] text-[color:var(--danger)]"
        : props.value === "running"
          ? "bg-[color:var(--blue-soft)] text-[color:var(--accent-blue)]"
          : props.value === "blocked"
            ? "bg-[color:var(--warning-soft)] text-[color:var(--warning)]"
            : "bg-[color:var(--block)] text-[color:var(--muted-strong)]";

  return (
    <Badge variant="outline" className={cn("h-auto gap-1 rounded-full px-2 py-0.5 text-[12px] font-medium", tone)}>
      {props.value === "running" ? <LoaderCircle data-icon className="animate-spin" /> : <Circle data-icon />}
      {props.value}
    </Badge>
  );
}

export function EmptyState(props: { title: string; body: string; icon?: LucideIcon }) {
  const Icon = props.icon || Archive;
  return (
    <div className="rounded-[10px] bg-[color:var(--block)] px-6 py-8 text-center shadow-[inset_0_0_0_1px_var(--line)]">
      <div className="mx-auto grid size-9 place-items-center rounded-[9px] bg-[color:var(--paper)] text-[color:var(--muted)] shadow-[inset_0_0_0_1px_var(--line)]">
        <Icon data-icon />
      </div>
      <h3 className="mt-3 text-[15px] font-semibold">{props.title}</h3>
      <p className="mx-auto mt-1.5 max-w-[52ch] text-[13px] leading-6 text-[color:var(--muted)] text-pretty">{props.body}</p>
    </div>
  );
}

export function CollectionEmptyState(props: {
  title: string;
  body: string;
  icon?: LucideIcon;
  action?: ReactNode;
}) {
  const Icon = props.icon || Archive;
  return (
    <div className="rounded-[10px] bg-[color:var(--surface)] px-6 py-10 shadow-[inset_0_0_0_1px_var(--line)]">
      <div className="mx-auto flex max-w-[460px] flex-col items-center text-center">
        <div className="grid size-9 place-items-center rounded-[9px] bg-[color:var(--paper)] text-[color:var(--muted)] shadow-[inset_0_0_0_1px_var(--line)]">
          <Icon data-icon />
        </div>
        <h2 className="mt-4 text-[22px] font-semibold leading-tight text-[color:var(--text)]">
          {props.title}
        </h2>
        <p className="mt-2 max-w-[34ch] text-[14px] leading-6 text-[color:var(--muted)] text-pretty">
          {props.body}
        </p>
        {props.action ? <div className="mt-5 flex justify-center">{props.action}</div> : null}
      </div>
    </div>
  );
}

export function Notice(props: { tone?: "info" | "danger"; children: ReactNode }) {
  const tone = props.tone || "info";
  return (
    <Alert
      variant={tone === "danger" ? "destructive" : "default"}
      className={cn(
        "rounded-[8px] border-0 px-3 py-2.5 text-[13px] leading-5 shadow-[inset_0_0_0_1px_var(--line)]",
        tone === "info" && "bg-[color:var(--block)] text-[color:var(--muted-strong)]",
        tone === "danger" && "bg-[color:var(--danger-soft)] text-[color:var(--danger)]",
      )}
    >
      {tone === "danger" ? <CircleAlert data-icon className="mt-0.5 shrink-0" /> : <MessageSquareText data-icon className="mt-0.5 shrink-0" />}
      <AlertDescription className="text-[13px] leading-5 text-inherit">
        {props.children}
      </AlertDescription>
    </Alert>
  );
}

export function DataBlock(props: PropsWithChildren<{ label: string; icon?: LucideIcon; className?: string }>) {
  const Icon = props.icon || Files;
  return (
    <div className={cn("rounded-[7px] bg-[color:var(--block-subtle)] px-2.5 py-2 shadow-[inset_0_0_0_1px_var(--line)]", props.className)}>
      <div className="mb-1 flex items-center gap-1.5 text-[12px] font-medium text-[color:var(--muted-strong)]">
        <Icon data-icon />
        {props.label}
      </div>
      <div className="text-[13px] leading-6 text-[color:var(--muted)]">{props.children}</div>
    </div>
  );
}

export function CodeBlock(props: PropsWithChildren<{ empty?: string; className?: string }>) {
  return (
    <div
      className={cn(
        "overflow-auto rounded-[9px] bg-[color:var(--code-bg)] px-4 py-3 font-mono text-[12px] leading-6 text-[color:var(--code-text)] shadow-[inset_0_0_0_1px_rgba(255,255,255,0.04)]",
        props.className,
      )}
    >
      {props.children || <div className="text-[color:var(--code-muted)]">{props.empty}</div>}
    </div>
  );
}

export function InlineMeta(props: PropsWithChildren<{ icon?: LucideIcon }>) {
  const Icon = props.icon || Clock3;
  return (
    <span className="inline-flex items-center gap-1.5 text-[12px] leading-5 text-[color:var(--muted)]">
      <Icon data-icon />
      {props.children}
    </span>
  );
}

export { CheckCircle2, SquareTerminal };
