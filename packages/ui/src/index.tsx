import {
  Activity,
  Archive,
  ArrowLeft,
  ArrowRight,
  Bot,
  CheckCircle2,
  ChevronDown,
  ChevronRight,
  Circle,
  CircleAlert,
  Clock3,
  Cloud,
  Files,
  FolderKanban,
  GitBranch,
  Inbox,
  Layers3,
  LoaderCircle,
  LogOut,
  type LucideIcon,
  MessageSquarePlus,
  MessageSquareText,
  Plus,
  Search,
  Settings,
  Sparkles,
  SquareTerminal,
} from "lucide-react";
import {
  useEffect,
  useMemo,
  useRef,
  useState,
  type ComponentProps,
  type KeyboardEvent as ReactKeyboardEvent,
  type PropsWithChildren,
  type ReactNode,
} from "react";
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
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuShortcut,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger,
} from "./components/ui/dropdown-menu";
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
export { Switch } from "./components/ui/switch";
export { Textarea as ShadcnTextarea } from "./components/ui/textarea";

const sidebarItems = [
  { to: "/inbox", label: "Inbox", icon: Inbox },
  { to: "/issues", label: "Issues", icon: MessageSquareText },
  { to: "/agents", label: "Agents", icon: Bot },
  { to: "/clusters", label: "Clusters", icon: Cloud },
  { to: "/projects", label: "Projects", icon: FolderKanban },
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

export type ShellActiveWorkItem = {
  issueId: string;
  projectName: string;
  title: string;
  status: string;
  namespace?: string;
  namespaceStatus?: string;
  sessionStatus?: string;
};

export type ShellAccount = {
  status: "signed-out" | "loading" | "signed-in" | "error";
  name?: string;
  email?: string;
  avatarUrl?: string;
  workspaceName?: string;
  error?: string;
  actionLabel?: string;
};

export type ShellSearchItem = {
  id: string;
  kind: "Issue" | "Project";
  title: string;
  subtitle?: string;
  keywords?: string[];
  to: string;
  params?: Record<string, string>;
  search?: Record<string, unknown>;
};

export function AppShell(
  props: {
    brandLogoSrc?: string;
    activeWorkItems?: ShellActiveWorkItem[];
    inboxUnreadCount?: number;
    searchItems?: ShellSearchItem[];
    searchLoading?: boolean;
    account?: ShellAccount;
    onSignIn?: () => void;
    onSignOut?: () => void;
  } = {},
) {
  const activeWorkItems = props.activeWorkItems || [];
  const [searchOpen, setSearchOpen] = useState(false);

  useEffect(() => {
    function openFromShortcut(event: globalThis.KeyboardEvent) {
      if (event.defaultPrevented || event.key.toLowerCase() !== "k") return;
      if (!event.metaKey && !event.ctrlKey) return;
      event.preventDefault();
      setSearchOpen(true);
    }

    document.addEventListener("keydown", openFromShortcut);
    return () => document.removeEventListener("keydown", openFromShortcut);
  }, []);

  return (
    <div className="grid h-full min-h-0 grid-cols-[252px_minmax(0,1fr)] bg-[color:var(--canvas)] text-[color:var(--text)]">
      <div className="app-titlebar" aria-hidden="true" />
      <aside className="flex min-h-0 flex-col border-r border-[color:var(--line)] bg-[color:var(--sidebar)] px-3 pb-4 pt-12">
        <WorkspaceMenu
          brandLogoSrc={props.brandLogoSrc}
          account={props.account}
          onSignIn={props.onSignIn}
          onSignOut={props.onSignOut}
        />

        <button
          type="button"
          className="mb-2 w-full rounded-[10px] bg-[color:var(--paper)] px-2.5 py-2 text-left shadow-[inset_0_0_0_1px_var(--line)] transition-[background-color,transform] duration-150 ease-out hover:bg-[color:var(--hover)] active:scale-[0.99]"
          onClick={() => setSearchOpen(true)}
        >
          <span className="flex items-center gap-2 text-[12px] text-[color:var(--muted)]">
            <Search data-icon className="shrink-0" />
            <span className="min-w-0 flex-1 truncate">Search issues and projects</span>
            <kbd className="shrink-0 rounded-[5px] bg-[color:var(--block)] px-1.5 py-0.5 text-[10px] font-medium leading-4 text-[color:var(--faint)] shadow-[inset_0_0_0_1px_var(--line)]">
              Cmd K
            </kbd>
          </span>
        </button>
        <SidebarActionLink to="/issues" search={{ new: "1" }} icon={MessageSquarePlus}>
          New issue
        </SidebarActionLink>

        <nav className="flex flex-col gap-1">
          {sidebarItems.map((item) => (
            <SidebarLink
              key={item.to}
              to={item.to}
              icon={item.icon}
              badgeCount={item.to === "/inbox" ? props.inboxUnreadCount : undefined}
            >
              {item.label}
            </SidebarLink>
          ))}
        </nav>

        <div className="mt-5 flex items-center justify-between px-2 text-[12px] font-medium text-[color:var(--faint)]">
          <span>Active work</span>
          <Activity data-icon />
        </div>
        <div className="mt-2 flex flex-col gap-1">
          {activeWorkItems.length > 0 ? (
            activeWorkItems.slice(0, 5).map((item) => <ActiveWorkLink key={item.issueId} item={item} />)
          ) : (
            <div className="rounded-[8px] px-2 py-2 text-[12px] leading-5 text-[color:var(--muted)]">
              No active issue work.
            </div>
          )}
        </div>

        <div className="mt-auto flex flex-col gap-2">
          <div className="rounded-[10px] bg-[color:var(--paper)] px-3 py-3 shadow-[inset_0_0_0_1px_var(--line)]">
            <div className="flex items-center gap-2 text-[12px] font-medium">
              <SquareTerminal data-icon className="text-[color:var(--muted)]" />
              Local runner
            </div>
            <p className="mt-1.5 text-[12px] leading-5 text-[color:var(--muted)]">
              Edits stay local. Kubernetes evidence stays attached to the issue.
            </p>
          </div>
        </div>
      </aside>
      <main className="min-h-0 min-w-0 bg-[color:var(--paper)]">
        <ScrollArea className="h-full">
          <div className="min-h-full pt-7">
            <Outlet />
          </div>
        </ScrollArea>
      </main>
      {searchOpen ? (
        <GlobalSearchDialog
          items={props.searchItems || []}
          loading={props.searchLoading}
          onClose={() => setSearchOpen(false)}
        />
      ) : null}
    </div>
  );
}

const maxGlobalSearchResults = 12;
const searchKindIcons: Record<ShellSearchItem["kind"], LucideIcon> = {
  Issue: MessageSquareText,
  Project: FolderKanban,
};

function GlobalSearchDialog(props: { items: ShellSearchItem[]; loading?: boolean; onClose: () => void }) {
  const router = useRouter();
  const inputRef = useRef<HTMLInputElement>(null);
  const [query, setQuery] = useState("");
  const [activeIndex, setActiveIndex] = useState(0);
  const normalizedQuery = query.trim().toLowerCase();
  const filteredItems = useMemo(() => {
    const tokens = normalizedQuery.split(/\s+/).filter(Boolean);
    const matched =
      tokens.length === 0
        ? props.items
        : props.items.filter((item) => {
            const haystack = [item.kind, item.title, item.subtitle, ...(item.keywords || [])]
              .filter(Boolean)
              .join(" ")
              .toLowerCase();
            return tokens.every((token) => haystack.includes(token));
          });

    return matched.slice(0, maxGlobalSearchResults);
  }, [normalizedQuery, props.items]);

  useEffect(() => {
    inputRef.current?.focus();
  }, []);

  useEffect(() => {
    setActiveIndex(0);
  }, [normalizedQuery]);

  useEffect(() => {
    setActiveIndex((index) => Math.min(index, Math.max(filteredItems.length - 1, 0)));
  }, [filteredItems.length]);

  useEffect(() => {
    function closeOnEscape(event: globalThis.KeyboardEvent) {
      if (event.key !== "Escape") return;
      event.preventDefault();
      props.onClose();
    }

    document.addEventListener("keydown", closeOnEscape);
    return () => document.removeEventListener("keydown", closeOnEscape);
  }, [props.onClose]);

  function navigateToItem(item: ShellSearchItem) {
    props.onClose();
    void router.navigate({
      to: item.to,
      params: item.params || {},
      search: item.search || {},
    } as never);
  }

  function handleInputKeyDown(event: ReactKeyboardEvent<HTMLInputElement>) {
    if (event.key === "ArrowDown") {
      event.preventDefault();
      setActiveIndex((index) => Math.min(index + 1, Math.max(filteredItems.length - 1, 0)));
      return;
    }

    if (event.key === "ArrowUp") {
      event.preventDefault();
      setActiveIndex((index) => Math.max(index - 1, 0));
      return;
    }

    if (event.key === "Enter") {
      const selectedItem = filteredItems[activeIndex];
      if (!selectedItem) return;
      event.preventDefault();
      navigateToItem(selectedItem);
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-start justify-center bg-[rgba(0,0,0,0.18)] px-4 pt-[12vh]">
      <button
        type="button"
        aria-label="Close global search"
        className="absolute inset-0 cursor-default"
        onClick={props.onClose}
      />
      <div
        role="dialog"
        aria-modal="true"
        aria-label="Global search"
        className="relative z-10 w-full max-w-[640px] overflow-hidden rounded-[12px] bg-[color:var(--paper)] text-[color:var(--text)] shadow-[0_24px_80px_rgba(0,0,0,0.18),inset_0_0_0_1px_var(--line)]"
      >
        <div className="flex items-center gap-2 border-b border-[color:var(--line)] px-3 py-2">
          <Search data-icon className="shrink-0 text-[color:var(--faint)]" />
          <ShadcnInput
            ref={inputRef}
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            onKeyDown={handleInputKeyDown}
            placeholder="Search issues and projects"
            className="h-9 min-h-9 border-0 bg-transparent px-0 text-[14px] shadow-none placeholder:text-[color:var(--faint)] focus-visible:border-transparent focus-visible:ring-0"
          />
          <kbd className="shrink-0 rounded-[5px] bg-[color:var(--block)] px-1.5 py-0.5 text-[10px] font-medium leading-4 text-[color:var(--faint)] shadow-[inset_0_0_0_1px_var(--line)]">
            Esc
          </kbd>
        </div>
        <div className="max-h-[420px] overflow-y-auto p-1.5">
          {filteredItems.length > 0 ? (
            <div className="flex flex-col gap-1">
              {filteredItems.map((item, index) => (
                <GlobalSearchResult
                  key={item.id}
                  item={item}
                  active={index === activeIndex}
                  onSelect={() => navigateToItem(item)}
                  onMouseEnter={() => setActiveIndex(index)}
                />
              ))}
            </div>
          ) : (
            <div className="px-3 py-8 text-center text-[13px] leading-5 text-[color:var(--muted)]">
              {props.loading ? "Loading workspace results." : "No matching workspace results."}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

function GlobalSearchResult(props: {
  item: ShellSearchItem;
  active: boolean;
  onSelect: () => void;
  onMouseEnter: () => void;
}) {
  const Icon = searchKindIcons[props.item.kind];

  return (
    <button
      type="button"
      className={cn(
        "flex min-h-12 w-full items-center gap-3 rounded-[8px] px-2.5 py-2 text-left transition-[background-color,transform] duration-150 ease-out active:scale-[0.995]",
        props.active ? "bg-[color:var(--selection)]" : "hover:bg-[color:var(--hover)]",
      )}
      onClick={props.onSelect}
      onMouseEnter={props.onMouseEnter}
    >
      <span className="grid size-8 shrink-0 place-items-center rounded-[8px] bg-[color:var(--block)] text-[color:var(--muted)] shadow-[inset_0_0_0_1px_var(--line)]">
        <Icon data-icon />
      </span>
      <span className="min-w-0 flex-1">
        <span className="block truncate text-[13px] font-medium leading-5 text-[color:var(--text)]">
          {props.item.title}
        </span>
        {props.item.subtitle ? (
          <span className="block truncate text-[12px] leading-5 text-[color:var(--muted)]">
            {props.item.subtitle}
          </span>
        ) : null}
      </span>
      <span className="shrink-0 rounded-[5px] bg-[color:var(--block)] px-1.5 py-0.5 text-[11px] font-medium leading-4 text-[color:var(--muted)] shadow-[inset_0_0_0_1px_var(--line)]">
        {props.item.kind}
      </span>
    </button>
  );
}

function WorkspaceMenu(props: { brandLogoSrc?: string; account?: ShellAccount; onSignIn?: () => void; onSignOut?: () => void }) {
  const account = props.account || { status: "signed-out" as const };
  const isBusy = account.status === "loading";
  const isSignedIn = account.status === "signed-in";
  const actionLabel = account.actionLabel || (isBusy ? "Waiting for GitHub" : "Sign in with GitHub");
  const [open, setOpen] = useState(false);
  const rootRef = useRef<HTMLDivElement>(null);
  const workspaceLabel = account.workspaceName || (isSignedIn ? "Personal workspace" : "Local workspace");
  const statusLabel = isSignedIn ? account.name || account.email || "Signed in" : isBusy ? "GitHub login pending" : "Not signed in";

  useEffect(() => {
    if (!open) return;
    function closeOnPointerDown(event: PointerEvent) {
      if (!rootRef.current?.contains(event.target as Node)) setOpen(false);
    }
    function closeOnEscape(event: KeyboardEvent) {
      if (event.key === "Escape") setOpen(false);
    }
    document.addEventListener("pointerdown", closeOnPointerDown);
    document.addEventListener("keydown", closeOnEscape);
    return () => {
      document.removeEventListener("pointerdown", closeOnPointerDown);
      document.removeEventListener("keydown", closeOnEscape);
    };
  }, [open]);

  return (
    <div ref={rootRef} className="relative mb-4">
      <button
        type="button"
        aria-haspopup="menu"
        aria-expanded={open}
        className="group flex min-h-10 w-full items-center gap-2.5 rounded-[8px] px-2 py-1.5 text-left transition-[background-color] duration-150 ease-out hover:bg-[color:var(--hover)]"
        onClick={() => setOpen((value) => !value)}
      >
        <div className="grid size-8 shrink-0 place-items-center overflow-hidden rounded-[8px] bg-[color:var(--paper)] text-[color:var(--ink)] shadow-[inset_0_0_0_1px_var(--line)]">
          {props.brandLogoSrc ? (
            <img src={props.brandLogoSrc} alt="" className="h-5 w-7 object-contain" />
          ) : (
            <span className="text-[13px] font-semibold">m</span>
          )}
        </div>
        <div className="min-w-0 flex-1">
          <div className="truncate text-[13px] font-semibold leading-5">{workspaceLabel}</div>
          <div className="truncate text-[11px] leading-4 text-[color:var(--muted)]">{statusLabel}</div>
        </div>
        <ChevronDown
          data-icon
          className="shrink-0 text-[color:var(--faint)] transition-[color] duration-150 ease-out group-hover:text-[color:var(--muted)]"
        />
      </button>

      {open ? (
        <div
          role="menu"
          className="absolute left-0 top-[calc(100%+8px)] z-30 w-[244px] overflow-hidden rounded-[12px] bg-[color:var(--paper)] p-1.5 shadow-[0_18px_45px_rgba(0,0,0,0.13),inset_0_0_0_1px_var(--line)]"
        >
          <div className="flex min-w-0 items-center gap-2.5 rounded-[9px] px-2.5 py-2.5">
            <div className="grid size-8 shrink-0 place-items-center overflow-hidden rounded-[8px] bg-[color:var(--block)] text-[color:var(--ink)] shadow-[inset_0_0_0_1px_var(--line)]">
              {props.brandLogoSrc ? (
                <img src={props.brandLogoSrc} alt="" className="h-5 w-7 object-contain" />
              ) : (
                <span className="text-[13px] font-semibold">m</span>
              )}
            </div>
            <div className="min-w-0 flex-1">
              <div className="truncate text-[13px] font-semibold leading-5 text-[color:var(--text)]">{workspaceLabel}</div>
              <div className="truncate text-[11px] leading-4 text-[color:var(--muted)]">
                {isSignedIn ? "Signed in workspace" : "Local workspace"}
              </div>
            </div>
          </div>

          <Link
            to="/settings"
            role="menuitem"
            className="group flex min-h-9 items-center gap-2 rounded-[8px] px-2.5 text-[12px] font-medium text-[color:var(--muted-strong)] transition-[background-color,color,transform] duration-150 ease-out hover:bg-[color:var(--hover)] hover:text-[color:var(--text)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[color:var(--focus)] active:scale-[0.99] [&_[data-icon]]:size-3.5"
            onClick={() => setOpen(false)}
          >
            <Settings data-icon className="text-[color:var(--faint)]" />
            <span className="min-w-0 flex-1 truncate">Workspace settings</span>
            <ChevronRight data-icon className="text-[color:var(--faint)] opacity-0 transition-opacity duration-150 group-hover:opacity-100" />
          </Link>

          {isSignedIn ? (
            <div className="mt-1 border-t border-[color:var(--line)] pt-1">
              <div className="mx-1 flex min-w-0 items-center gap-2.5 rounded-[8px] px-2 py-2">
                <UserAvatar name={account.name} avatarUrl={account.avatarUrl} size="sm" />
                <div className="min-w-0 flex-1 text-left">
                  <div className="truncate text-[12px] font-medium leading-5 text-[color:var(--text)]">{account.name || "mspace user"}</div>
                  <div className="truncate text-[11px] leading-4 text-[color:var(--muted)]">{account.email || "GitHub connected"}</div>
                </div>
              </div>
              <button
                type="button"
                role="menuitem"
                className="group flex min-h-9 w-full items-center gap-2 rounded-[8px] px-2.5 text-[12px] font-medium text-[color:var(--muted)] transition-[background-color,color,transform] duration-150 ease-out hover:bg-[color:var(--hover)] hover:text-[color:var(--text)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[color:var(--focus)] active:scale-[0.99] [&_[data-icon]]:size-3.5"
                onClick={() => {
                  setOpen(false);
                  props.onSignOut?.();
                }}
              >
                <LogOut data-icon className="text-[color:var(--faint)]" />
                <span>Sign out</span>
              </button>
            </div>
          ) : (
            <div className="mt-1 border-t border-[color:var(--line)] px-1 pt-2">
              <div className="mb-2 flex items-center gap-2 px-1.5 text-[12px] leading-5 text-[color:var(--muted)]">
                {isBusy ? <LoaderCircle data-icon className="animate-spin" /> : <GitBranch data-icon />}
                <span>{isBusy ? "Waiting for GitHub callback" : "GitHub identity not connected"}</span>
              </div>
              <ShadcnButton
                type="button"
                size="sm"
                variant="secondary"
                className="h-8 w-full justify-center gap-2 text-[12px]"
                disabled={isBusy}
                onClick={() => {
                  setOpen(false);
                  props.onSignIn?.();
                }}
              >
                {isBusy ? <LoaderCircle data-icon className="animate-spin" /> : <GitBranch data-icon />}
                <span>{actionLabel}</span>
              </ShadcnButton>
              {account.status === "error" && account.error ? (
                <p className="mt-2 line-clamp-2 px-1 text-[11px] leading-4 text-[color:var(--danger)]">{account.error}</p>
              ) : null}
            </div>
          )}
        </div>
      ) : null}
    </div>
  );
}

function UserAvatar(props: { name?: string; avatarUrl?: string; size?: "sm" | "md" }) {
  const [failed, setFailed] = useState(false);
  const initial = props.name?.trim().slice(0, 1).toUpperCase() || "M";
  const sizeClass = props.size === "sm" ? "size-7 text-[11px]" : "size-8 text-[12px]";

  useEffect(() => {
    setFailed(false);
  }, [props.avatarUrl]);

  return (
    <div className={cn("grid shrink-0 place-items-center overflow-hidden rounded-full bg-[color:var(--selection)] font-semibold text-[color:var(--muted)]", sizeClass)}>
      {props.avatarUrl && !failed ? (
        <img src={props.avatarUrl} alt="" className="size-full object-cover" onError={() => setFailed(true)} />
      ) : (
        <span>{initial}</span>
      )}
    </div>
  );
}

export function SidebarLink(props: PropsWithChildren<{ to: string; icon: LucideIcon; badgeCount?: number }>) {
  const Icon = props.icon;
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  const isActive = pathname === props.to || pathname.startsWith(`${props.to}/`);
  const badgeCount = Math.max(0, Math.trunc(props.badgeCount || 0));
  const badgeLabel = badgeCount > 99 ? "99+" : String(badgeCount);

  return (
    <Link
      to={props.to}
      className={cn(
        "group relative flex items-center gap-2 rounded-[7px] px-2 py-1.5 pr-8 text-[13px] font-medium transition-[background-color,color,transform] duration-150 ease-out active:scale-[0.98]",
        isActive
          ? "bg-[color:var(--selection)] text-[color:var(--text)]"
          : "text-[color:var(--muted)] hover:bg-[color:var(--hover)] hover:text-[color:var(--text)]",
      )}
    >
      <Icon data-icon />
      <span className="min-w-0 flex-1 truncate">{props.children}</span>
      {badgeCount > 0 ? (
        <span
          aria-label={`${badgeCount} unread Inbox update${badgeCount === 1 ? "" : "s"}`}
          className="absolute right-2 top-1/2 grid h-4 min-w-4 -translate-y-1/2 place-items-center rounded-full bg-[color:var(--inbox-badge)] px-1 text-[10px] font-semibold leading-4 text-[color:var(--inbox-badge-text)] tabular-nums"
        >
          {badgeLabel}
        </span>
      ) : null}
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

function ActiveWorkLink(props: { item: ShellActiveWorkItem }) {
  const status = normalizeStatusValue(props.item.sessionStatus || props.item.namespaceStatus || props.item.status);
  const secondary = props.item.namespace || props.item.projectName;
  return (
    <Link
      to="/issues/$issueId"
      params={{ issueId: props.item.issueId }}
      className="group flex w-full items-center gap-2 rounded-[7px] px-2 py-1.5 text-left text-[13px] transition-[background-color,transform] duration-150 ease-out hover:bg-[color:var(--hover)] active:scale-[0.98]"
    >
      <span
        className={cn(
          "size-2 rounded-full",
          status === "running" || status === "in_progress" || status === "deploying" || status === "test_in_progress" ? "bg-[color:var(--accent-blue)]" : null,
          status === "queued" || status === "cleanup_requested" ? "bg-[color:var(--warning)]" : null,
          status === "open" || status === "completed" || status === "test_passed" ? "bg-[color:var(--success)]" : null,
          status === "closed" ? "bg-[color:var(--done)]" : null,
          status === "retained" ? "bg-[color:var(--muted)]" : null,
          status === "needs_review" || status === "ready_for_test" || status === "changes_requested" ? "bg-[color:var(--warning)]" : null,
          status === "test_failed" || status === "failed" ? "bg-[color:var(--danger)]" : null,
          !["running", "in_progress", "deploying", "queued", "cleanup_requested", "open", "closed", "completed", "retained", "needs_review", "ready_for_test", "changes_requested", "test_in_progress", "test_passed", "test_failed", "failed"].includes(status) ? "bg-[color:var(--faint)]" : null,
        )}
      />
      <span className="min-w-0 flex-1">
        <span className="block truncate font-medium">{props.item.title}</span>
        <span className="block truncate text-[11px] text-[color:var(--muted)]">
          {props.item.projectName}{secondary && secondary !== props.item.projectName ? ` · ${secondary}` : ""}
        </span>
      </span>
      <ChevronRight
        data-icon
        className="opacity-0 transition-[opacity,transform] duration-150 ease-out group-hover:opacity-100"
      />
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

export function PageFrame(props: PropsWithChildren<{ title: string; subtitle?: ReactNode; actions?: ReactNode; breadcrumbs?: BreadcrumbItem[] }>) {
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
          {typeof props.subtitle === "string" ? (
            <p className="mt-3 max-w-[72ch] text-[14px] leading-6 text-[color:var(--muted)] text-pretty">
              {props.subtitle}
            </p>
          ) : props.subtitle ? (
            <div className="mt-4">
              {props.subtitle}
            </div>
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

export function StatusBadge(props: { value: string; className?: string; label?: string; valueLabel?: string }) {
  const normalizedValue = normalizeStatusValue(props.value);
  const tone =
    normalizedValue === "open" || normalizedValue === "completed" || normalizedValue === "test_passed"
      ? "bg-[color:var(--success-soft)] text-[color:var(--success)]"
      : normalizedValue === "closed"
        ? "bg-[color:var(--done-soft)] text-[color:var(--done)]"
        : normalizedValue === "failed" || normalizedValue === "test_failed" || normalizedValue === "deploy_failed" || normalizedValue === "cleanup_failed"
        ? "bg-[color:var(--danger-soft)] text-[color:var(--danger)]"
        : normalizedValue === "running" || normalizedValue === "in_progress" || normalizedValue === "test_in_progress" || normalizedValue === "deploying"
          ? "bg-[color:var(--blue-soft)] text-[color:var(--accent-blue)]"
          : normalizedValue === "blocked" || normalizedValue === "needs_review" || normalizedValue === "ready_for_test" || normalizedValue === "changes_requested" || normalizedValue === "cleanup_requested" || normalizedValue === "preview_unverified" || normalizedValue === "deploy_interrupted"
            ? "bg-[color:var(--warning-soft)] text-[color:var(--warning)]"
            : normalizedValue === "cancelled" || normalizedValue === "retained" || normalizedValue === "cleaned"
              ? "bg-[color:var(--block)] text-[color:var(--muted)]"
              : "bg-[color:var(--block)] text-[color:var(--muted-strong)]";
  const isRunning = normalizedValue === "running" || normalizedValue === "in_progress" || normalizedValue === "test_in_progress" || normalizedValue === "deploying";

  return (
    <Badge variant="outline" className={cn("h-auto max-w-full gap-1 rounded-full px-2 py-0.5 text-[12px] font-medium", tone, props.className)}>
      {props.label ? <span className="shrink-0 text-[color:var(--faint)]">{props.label}</span> : null}
      {isRunning ? <LoaderCircle data-icon className="animate-spin" /> : <Circle data-icon />}
      <span className="truncate">{props.valueLabel || statusLabel(normalizedValue)}</span>
    </Badge>
  );
}

function normalizeStatusValue(value: string) {
  const status = value.trim().toLowerCase();
  if (status === "review" || status === "in_review") return "needs_review";
  if (status === "testing") return "test_in_progress";
  if (status === "queued") return "in_progress";
  if (status === "done") return "closed";
  return status;
}

function statusLabel(value: string) {
  const labels: Record<string, string> = {
    open: "Open",
    in_progress: "In progress",
    running: "Running",
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
    completed: "Completed",
    deploying: "Deploying",
    preview_unverified: "Preview unverified",
    deploy_failed: "Deploy failed",
    deploy_interrupted: "Deploy interrupted",
    cleanup_requested: "Cleanup requested",
    cleanup_failed: "Cleanup failed",
    cleaned: "Cleaned",
    retained: "Retained",
  };
  if (labels[value]) return labels[value];
  return value
    .replace(/[_-]+/g, " ")
    .replace(/\b\w/g, (letter) => letter.toUpperCase());
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
