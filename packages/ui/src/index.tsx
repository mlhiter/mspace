import {
  Activity,
  Archive,
  ArrowLeft,
  ArrowRight,
  Bot,
  CheckCircle2,
  ChevronDown,
  ChevronRight,
  ClipboardCheck,
  Circle,
  CircleAlert,
  Clock3,
  Cloud,
  Files,
  FolderKanban,
  GitBranch,
  Inbox,
  Languages,
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
import { supportedMspaceLocales, t as translate, useMspaceLanguage, useMspaceTranslation, type MspaceLocale } from "@mspace/i18n";
import { Alert, AlertDescription } from "./components/ui/alert";
import { Badge } from "./components/ui/badge";
import {
  Button as ShadcnButton,
  type buttonVariants as shadcnButtonVariants,
} from "./components/ui/button";
import { Card, CardAction, CardContent, CardHeader, CardTitle } from "./components/ui/card";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuTrigger,
} from "./components/ui/dropdown-menu";
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
  { to: "/inbox", labelKey: "navigation.inbox", icon: Inbox },
  { to: "/issues", labelKey: "navigation.issues", icon: MessageSquareText },
  { to: "/tests", labelKey: "navigation.tests", icon: ClipboardCheck },
  { to: "/agents", labelKey: "navigation.agents", icon: Bot },
  { to: "/clusters", labelKey: "navigation.clusters", icon: Cloud },
  { to: "/projects", labelKey: "navigation.projects", icon: FolderKanban },
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
  projectName?: string;
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
  identityProvider?: string;
  identityLogin?: string;
  isServerAdmin?: boolean;
  workspaceName?: string;
  workspaceId?: string;
  workspaceIcon?: string;
  workspaceDescription?: string;
  workspaceKind?: string;
  workspaceRole?: string;
  workspaces?: Array<{ id: string; name: string; role?: string; kind?: string; icon?: string; description?: string }>;
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
    onSelectWorkspace?: (workspaceId: string) => void;
    onCreateTeamWorkspace?: () => void;
  } = {},
) {
  const activeWorkItems = props.activeWorkItems || [];
  const [searchOpen, setSearchOpen] = useState(false);
  const { t } = useMspaceTranslation();

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
          onSelectWorkspace={props.onSelectWorkspace}
          onCreateTeamWorkspace={props.onCreateTeamWorkspace}
        />

        <button
          type="button"
          className="mb-2 w-full rounded-[10px] bg-[color:var(--paper)] px-2.5 py-2 text-left shadow-[inset_0_0_0_1px_var(--line)] transition-[background-color,transform] duration-150 ease-out hover:bg-[color:var(--hover)] active:scale-[0.99]"
          onClick={() => setSearchOpen(true)}
        >
          <span className="flex items-center gap-2 text-[12px] text-[color:var(--muted)]">
            <Search data-icon className="shrink-0" />
            <span className="min-w-0 flex-1 truncate">{t("navigation.searchPlaceholder")}</span>
            <kbd className="shrink-0 rounded-[5px] bg-[color:var(--block)] px-1.5 py-0.5 text-[10px] font-medium leading-4 text-[color:var(--faint)] shadow-[inset_0_0_0_1px_var(--line)]">
              Cmd K
            </kbd>
          </span>
        </button>
        <SidebarActionLink to="/issues" search={{ new: "1" }} icon={MessageSquarePlus}>
          {t("navigation.newIssue")}
        </SidebarActionLink>

        <nav className="flex flex-col gap-1">
          {sidebarItems.map((item) => (
            <SidebarLink
              key={item.to}
              to={item.to}
              icon={item.icon}
              badgeCount={item.to === "/inbox" ? props.inboxUnreadCount : undefined}
            >
              {t(item.labelKey)}
            </SidebarLink>
          ))}
        </nav>

        <div className="mt-5 flex items-center justify-between px-2 text-[12px] font-medium text-[color:var(--faint)]">
          <span>{t("navigation.activeWork")}</span>
          <Activity data-icon />
        </div>
        <div className="mt-2 flex flex-col gap-1">
          {activeWorkItems.length > 0 ? (
            activeWorkItems.slice(0, 5).map((item) => <ActiveWorkLink key={item.issueId} item={item} />)
          ) : (
            <div className="rounded-[8px] px-2 py-2 text-[12px] leading-5 text-[color:var(--muted)]">
              {t("navigation.noActiveWork")}
            </div>
          )}
        </div>

        <div className="mt-auto flex flex-col gap-2">
          <div className="rounded-[10px] bg-[color:var(--paper)] px-3 py-3 shadow-[inset_0_0_0_1px_var(--line)]">
            <div className="flex items-center gap-2 text-[12px] font-medium">
              <SquareTerminal data-icon className="text-[color:var(--muted)]" />
              {t("navigation.localRunner")}
            </div>
            <p className="mt-1.5 text-[12px] leading-5 text-[color:var(--muted)]">
              {t("navigation.localRunnerDescription")}
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
  const { t } = useMspaceTranslation();
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
        aria-label={t("search.close")}
        className="absolute inset-0 cursor-default"
        onClick={props.onClose}
      />
      <div
        role="dialog"
        aria-modal="true"
        aria-label={t("search.dialogLabel")}
        className="relative z-10 w-full max-w-[640px] overflow-hidden rounded-[12px] bg-[color:var(--paper)] text-[color:var(--text)] shadow-[0_24px_80px_rgba(0,0,0,0.18),inset_0_0_0_1px_var(--line)]"
      >
        <div className="flex items-center gap-2 border-b border-[color:var(--line)] px-3 py-2">
          <Search data-icon className="shrink-0 text-[color:var(--faint)]" />
          <ShadcnInput
            ref={inputRef}
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            onKeyDown={handleInputKeyDown}
            placeholder={t("search.placeholder")}
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
              {props.loading ? t("search.loading") : t("search.noResults")}
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
  const { t } = useMspaceTranslation();

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
        {t(`search.kind.${props.item.kind.toLowerCase()}`)}
      </span>
    </button>
  );
}

function WorkspaceMenu(props: {
  brandLogoSrc?: string;
  account?: ShellAccount;
  onSignIn?: () => void;
  onSignOut?: () => void;
  onSelectWorkspace?: (workspaceId: string) => void;
  onCreateTeamWorkspace?: () => void;
}) {
  const account = props.account || { status: "signed-out" as const };
  const { t } = useMspaceTranslation();
  const isBusy = account.status === "loading";
  const isSignedIn = account.status === "signed-in";
  const actionLabel = account.actionLabel || (isBusy ? t("workspace.waitingForGitHub") : t("workspace.signInWithGitHub"));
  const [open, setOpen] = useState(false);
  const rootRef = useRef<HTMLDivElement>(null);
  const workspaceLabel = account.workspaceName || (isSignedIn ? t("workspace.personalWorkspace") : t("workspace.localWorkspace"));
  const workspaceKindLabel = workspaceKindLabelFor(account.workspaceKind);
  const workspaceDescription = account.workspaceDescription?.trim();
  const statusLabel = isSignedIn ? account.name || account.email || t("workspace.signedIn") : isBusy ? t("workspace.githubLoginPending") : t("workspace.notSignedIn");
  const accountIdentityLabel = accountIdentityLabelFor(account);
  const workspaces = account.workspaces || [];

  useEffect(() => {
    if (!open) return;
    function closeOnPointerDown(event: PointerEvent) {
      const target = event.target as HTMLElement | null;
      if (target?.closest("[data-mspace-language-menu]")) return;
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
        <WorkspaceMark
          name={workspaceLabel}
          icon={account.workspaceIcon}
          brandLogoSrc={props.brandLogoSrc}
          size="md"
        />
        <div className="min-w-0 flex-1">
          <div className="truncate text-[13px] font-semibold leading-5">{workspaceLabel}</div>
          <div className="truncate text-[11px] leading-4 text-[color:var(--muted)]">
            {workspaceKindLabel ? `${workspaceKindLabel} · ${statusLabel}` : statusLabel}
          </div>
        </div>
        <ChevronDown
          data-icon
          className="shrink-0 text-[color:var(--faint)] transition-[color] duration-150 ease-out group-hover:text-[color:var(--muted)]"
        />
      </button>

      {open ? (
        <div
          role="menu"
          className="absolute left-0 top-[calc(100%+7px)] z-30 w-[252px] max-w-[calc(100vw-24px)] overflow-hidden rounded-[10px] bg-[color:var(--paper)] p-1 shadow-[0_16px_36px_rgba(0,0,0,0.12),inset_0_0_0_1px_var(--line)]"
        >
          <div className="flex min-w-0 items-center gap-2 rounded-[8px] px-2 py-2">
            <WorkspaceMark
              name={workspaceLabel}
              icon={account.workspaceIcon}
              brandLogoSrc={props.brandLogoSrc}
              size="sm"
            />
            <div className="min-w-0 flex-1">
              <div className="truncate text-[12px] font-semibold leading-5 text-[color:var(--text)]">{workspaceLabel}</div>
              <div className="truncate text-[11px] leading-4 text-[color:var(--muted)]">
                {workspaceDescription || (isSignedIn ? `${workspaceKindLabel || t("workspace.workspace")} · ${workspaceRoleLabelFor(account.workspaceRole)}` : t("workspace.localWorkspace"))}
              </div>
            </div>
          </div>

          {isSignedIn && workspaces.length > 1 ? (
            <div className="border-t border-[color:var(--line)] px-1 py-1">
              <div className="px-1.5 pb-1 pt-0.5 text-[11px] font-medium leading-4 text-[color:var(--faint)]">{t("workspace.workspaces")}</div>
              <div className="grid gap-px">
                {workspaces.map((workspace) => {
                  const selected = workspace.id === account.workspaceId;
                  return (
                    <button
                      key={workspace.id}
                      type="button"
                      role="menuitemradio"
                      aria-checked={selected}
                      className={cn(
                        "group grid min-h-9 w-full grid-cols-[24px_minmax(0,1fr)_16px] items-center gap-2 rounded-[7px] px-1.5 py-1 text-left text-[12px] font-medium transition-[background-color,color,transform] duration-150 ease-out hover:bg-[color:var(--hover)] hover:text-[color:var(--text)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[color:var(--focus)] active:scale-[0.99] [&_[data-icon]]:size-3.5",
                        selected ? "bg-[color:var(--selection)] text-[color:var(--text)]" : "text-[color:var(--muted-strong)]",
                      )}
                      onClick={() => {
                        setOpen(false);
                        props.onSelectWorkspace?.(workspace.id);
                      }}
                    >
                      <WorkspaceMark name={workspace.name} icon={workspace.icon} size="xs" />
                      <span className="min-w-0 overflow-hidden">
                        <span className="block truncate">{workspace.name}</span>
                        <span className="block truncate text-[11px] font-normal leading-4 text-[color:var(--muted)]">
                          {workspace.description?.trim() || `${workspaceKindLabelFor(workspace.kind)} · ${workspaceRoleLabelFor(workspace.role)}`}
                        </span>
                      </span>
                      <span className="grid size-4 place-items-center">
                        {selected ? <CheckCircle2 data-icon className="text-[color:var(--success)]" /> : null}
                      </span>
                    </button>
                  );
                })}
              </div>
            </div>
          ) : null}

          {isSignedIn ? (
            <div className="grid gap-px border-t border-[color:var(--line)] px-1 py-1">
              {account.isServerAdmin ? (
                <WorkspaceMenuAction
                  icon={Plus}
                  label={t("workspace.createTeamWorkspace")}
                  onClick={() => {
                    setOpen(false);
                    props.onCreateTeamWorkspace?.();
                  }}
                />
              ) : null}
              <WorkspaceMenuLink
                to="/settings"
                icon={Settings}
                label={t("workspace.workspaceSettings")}
                trailingIcon={ChevronRight}
                onClick={() => setOpen(false)}
              />
            </div>
          ) : (
            <WorkspaceMenuLink
              to="/settings"
              icon={Settings}
              label={t("workspace.workspaceSettings")}
              trailingIcon={ChevronRight}
              className="mx-1"
              onClick={() => setOpen(false)}
            />
          )}

          {isSignedIn ? (
            <div className="mt-1 border-t border-[color:var(--line)] px-1 py-1">
              <div className="flex min-w-0 items-center gap-2 rounded-[7px] px-1.5 py-1.5">
                <UserAvatar name={account.name} avatarUrl={account.avatarUrl} size="sm" />
                <div className="min-w-0 flex-1 text-left">
                  <div className="truncate text-[12px] font-medium leading-5 text-[color:var(--text)]">{account.name || t("workspace.mspaceUser")}</div>
                  <div className="truncate text-[11px] leading-4 text-[color:var(--muted)]">{accountIdentityLabel}</div>
                </div>
              </div>
              <LanguageMenuItem />
              <WorkspaceMenuAction
                icon={LogOut}
                label={t("workspace.signOut")}
                muted
                onClick={() => {
                  setOpen(false);
                  props.onSignOut?.();
                }}
              />
            </div>
          ) : (
            <div className="mt-1 border-t border-[color:var(--line)] px-1 py-1">
              <div className="mb-1 flex items-center gap-2 px-1.5 text-[12px] leading-5 text-[color:var(--muted)]">
                {isBusy ? <LoaderCircle data-icon className="animate-spin" /> : <GitBranch data-icon />}
                <span>{isBusy ? t("workspace.waitingForGitHubCallback") : t("workspace.githubIdentityNotConnected")}</span>
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

function WorkspaceMenuAction(props: {
  icon: LucideIcon;
  label: string;
  muted?: boolean;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      role="menuitem"
      className={cn(
        "group grid min-h-8 w-full grid-cols-[24px_minmax(0,1fr)_16px] items-center rounded-[7px] px-1 text-left transition-[background-color,color,transform] duration-150 ease-out hover:bg-[color:var(--hover)] hover:text-[color:var(--text)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[color:var(--focus)] active:scale-[0.99]",
        props.muted ? "text-[color:var(--muted)]" : "text-[color:var(--muted-strong)]",
      )}
      onClick={props.onClick}
    >
      <WorkspaceMenuIcon icon={props.icon} />
      <WorkspaceMenuLabel>{props.label}</WorkspaceMenuLabel>
      <span aria-hidden="true" />
    </button>
  );
}

function WorkspaceMenuLink(props: {
  to: string;
  icon: LucideIcon;
  label: string;
  trailingIcon?: LucideIcon;
  className?: string;
  onClick: () => void;
}) {
  const TrailingIcon = props.trailingIcon;
  return (
    <Link
      to={props.to}
      role="menuitem"
      className={cn(
        "group grid min-h-8 grid-cols-[24px_minmax(0,1fr)_16px] items-center rounded-[7px] px-1 text-[color:var(--muted-strong)] transition-[background-color,color,transform] duration-150 ease-out hover:bg-[color:var(--hover)] hover:text-[color:var(--text)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[color:var(--focus)] active:scale-[0.99]",
        props.className,
      )}
      onClick={props.onClick}
    >
      <WorkspaceMenuIcon icon={props.icon} />
      <WorkspaceMenuLabel>{props.label}</WorkspaceMenuLabel>
      {TrailingIcon ? (
        <TrailingIcon data-icon className="size-3.5 justify-self-center text-[color:var(--faint)] opacity-0 transition-opacity duration-150 group-hover:opacity-100" />
      ) : (
        <span aria-hidden="true" />
      )}
    </Link>
  );
}

function WorkspaceMenuIcon(props: { icon: LucideIcon }) {
  const Icon = props.icon;
  return (
    <span className="grid size-6 place-items-center text-[color:var(--faint)]">
      <Icon data-icon className="size-3.5" />
    </span>
  );
}

function WorkspaceMenuLabel(props: PropsWithChildren) {
  return <span className="min-w-0 truncate text-[12px] font-medium leading-4">{props.children}</span>;
}

function LanguageMenuItem() {
  const { t } = useMspaceTranslation();
  const { language, changeLanguage } = useMspaceLanguage();
  const activeLocale = supportedMspaceLocales.find((locale) => locale.code === language) || supportedMspaceLocales[0];

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <button
          type="button"
          className="group grid min-h-8 w-full grid-cols-[24px_minmax(0,1fr)_auto] items-center rounded-[7px] px-1 text-left text-[color:var(--muted-strong)] transition-[background-color,color,transform] duration-150 ease-out hover:bg-[color:var(--hover)] hover:text-[color:var(--text)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[color:var(--focus)] active:scale-[0.99]"
        >
          <WorkspaceMenuIcon icon={Languages} />
          <WorkspaceMenuLabel>{t("language.label")}</WorkspaceMenuLabel>
          <span className="ml-2 inline-flex min-w-0 max-w-[88px] items-center gap-1 justify-self-end rounded-[6px] bg-[color:var(--block)] px-1.5 py-0.5 text-[11px] font-medium leading-4 text-[color:var(--muted)] shadow-[inset_0_0_0_1px_var(--line)]">
            <span className="truncate">{t(`language.${activeLocale.code}`)}</span>
            <ChevronRight data-icon className="size-3 shrink-0 text-[color:var(--faint)] transition-transform duration-150 group-data-[state=open]:rotate-90" />
          </span>
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent data-mspace-language-menu side="right" align="start" sideOffset={10} className="min-w-[176px]">
        <DropdownMenuLabel>{t("language.label")}</DropdownMenuLabel>
        <DropdownMenuRadioGroup value={language} onValueChange={(value: string) => void changeLanguage(value as MspaceLocale)}>
          {supportedMspaceLocales.map((locale) => (
            <DropdownMenuRadioItem
              key={locale.code}
              value={locale.code}
              className="data-[state=checked]:bg-[color:var(--selection)] data-[state=checked]:text-[color:var(--text)]"
            >
              <span className="min-w-0 flex-1 truncate">{t(`language.${locale.code}`)}</span>
              <span className="text-[11px] leading-4 text-[color:var(--faint)]">{locale.code}</span>
            </DropdownMenuRadioItem>
          ))}
        </DropdownMenuRadioGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function workspaceKindLabelFor(kind: string | undefined) {
  const normalized = kind?.trim().toLowerCase();
  if (normalized === "team") return translate("workspace.kind.team");
  if (normalized === "personal") return translate("workspace.kind.personal");
  if (normalized) return statusLabel(normalized);
  return "";
}

function workspaceRoleLabelFor(role: string | undefined) {
  const normalized = role?.trim().toLowerCase() || "member";
  if (normalized === "owner" || normalized === "admin" || normalized === "member") {
    return translate(`workspace.role.${normalized}`);
  }
  return normalized;
}

function accountIdentityLabelFor(account: ShellAccount) {
  const provider = account.identityProvider?.trim().toLowerCase();
  if (provider === "github") {
    return account.identityLogin ? translate("workspace.githubIdentityConnectedAs", { login: account.identityLogin }) : translate("workspace.githubConnected");
  }
  if (provider === "password") {
    return account.identityLogin ? translate("workspace.localAccountSignedInAs", { login: account.identityLogin }) : translate("workspace.localAccount");
  }
  return account.email || translate("workspace.signedIn");
}

function WorkspaceMark(props: { name?: string; icon?: string; brandLogoSrc?: string; size?: "xs" | "sm" | "md" }) {
  const mark = props.icon?.trim();
  const initial = props.name?.trim().slice(0, 1).toUpperCase() || "m";
  const sizeClass =
    props.size === "xs"
      ? "size-6 rounded-[6px] text-[11px]"
      : props.size === "sm"
        ? "size-7 rounded-[7px] text-[12px]"
        : "size-8 rounded-[8px] text-[13px]";
  const logoClass = props.size === "sm" ? "h-4 w-6" : "h-5 w-7";

  return (
    <span className={cn("grid shrink-0 place-items-center overflow-hidden bg-[color:var(--paper)] font-semibold text-[color:var(--ink)] shadow-[inset_0_0_0_1px_var(--line)]", sizeClass)}>
      {mark ? (
        <span className="max-w-full truncate px-1 leading-none">{mark}</span>
      ) : props.brandLogoSrc ? (
        <img src={props.brandLogoSrc} alt="" className={cn("object-contain", logoClass)} />
      ) : (
        <span>{initial}</span>
      )}
    </span>
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
  const { t } = useMspaceTranslation();
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
          aria-label={`${badgeCount} ${t("navigation.inbox")}`}
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
  const { t } = useMspaceTranslation();
  const projectName = props.item.projectName || t("common.noProject");
  const secondary = props.item.namespace || projectName;
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
          {projectName}{secondary && secondary !== projectName ? ` · ${secondary}` : ""}
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
  const { t } = useMspaceTranslation();
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
        aria-label={t("navigation.goBack")}
        disabled={!canGoBack}
        className="grid size-7 place-items-center rounded-[7px] text-[color:var(--muted)] transition-[background-color,color,transform,opacity] duration-150 ease-out hover:bg-[color:var(--hover)] hover:text-[color:var(--text)] active:scale-95 disabled:pointer-events-none disabled:opacity-35"
        onClick={() => router.history.back()}
      >
        <ArrowLeft data-icon />
      </button>
      <button
        type="button"
        aria-label={t("navigation.goForward")}
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
  const { t } = useMspaceTranslation();
  return (
    <nav aria-label={t("navigation.breadcrumb")} className="min-w-0 flex-1 overflow-hidden">
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
  const { t } = useMspaceTranslation();
  const breadcrumbs = props.breadcrumbs || [
    { label: t("common.mspace"), to: "/inbox" },
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
  const isRunning = normalizedValue === "running" || normalizedValue === "in_progress" || normalizedValue === "test_in_progress" || normalizedValue === "deploying" || normalizedValue.startsWith("team_runtime_");

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
  const translated = translate(`issueStatus.${value}`, { defaultValue: "" });
  if (translated) return translated;
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
