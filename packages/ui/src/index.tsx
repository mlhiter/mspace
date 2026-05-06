import type {
  InputHTMLAttributes,
  PropsWithChildren,
  ReactNode,
  TextareaHTMLAttributes,
} from "react";
import { NavLink, Outlet } from "react-router-dom";
import { clsx } from "clsx";

export function AppShell() {
  return (
    <div className="grid h-full grid-cols-[240px_1fr]">
      <aside className="border-r border-[color:var(--border)] bg-[color:var(--panel)]/80 px-4 py-5 backdrop-blur">
        <div className="mb-8">
          <div className="text-xs font-medium uppercase tracking-[0.18em] text-[color:var(--muted)]">
            mspace
          </div>
          <h1 className="mt-2 text-2xl font-semibold">Local-first agent workspace</h1>
          <p className="mt-2 text-sm leading-6 text-[color:var(--muted)]">
            Develop locally, validate in Kubernetes, keep the whole story on the issue.
          </p>
        </div>
        <nav className="space-y-2">
          <SidebarLink to="/inbox">Inbox</SidebarLink>
          <SidebarLink to="/projects">Projects</SidebarLink>
        </nav>
      </aside>
      <main className="overflow-auto">
        <Outlet />
      </main>
    </div>
  );
}

export function SidebarLink(props: PropsWithChildren<{ to: string }>) {
  return (
    <NavLink
      to={props.to}
      className={({ isActive }) =>
        clsx(
          "flex rounded-lg px-3 py-2 text-sm transition-colors",
          isActive
            ? "bg-[color:var(--accent-soft)] text-[color:var(--accent)]"
            : "text-[color:var(--muted)] hover:bg-white/60 hover:text-[color:var(--text)]",
        )
      }
    >
      {props.children}
    </NavLink>
  );
}

export function PageFrame(props: PropsWithChildren<{ title: string; subtitle?: string; actions?: ReactNode }>) {
  return (
    <div className="min-h-full px-8 py-8">
      <div className="mb-8 flex items-start justify-between gap-6">
        <div>
          <div className="text-xs font-medium uppercase tracking-[0.18em] text-[color:var(--muted)]">
            mspace
          </div>
          <h2 className="mt-2 text-3xl font-semibold">{props.title}</h2>
          {props.subtitle ? (
            <p className="mt-3 max-w-3xl text-sm leading-6 text-[color:var(--muted)]">
              {props.subtitle}
            </p>
          ) : null}
        </div>
        {props.actions ? <div className="flex gap-3">{props.actions}</div> : null}
      </div>
      {props.children}
    </div>
  );
}

export function Panel(props: PropsWithChildren<{ title?: string; aside?: ReactNode; className?: string }>) {
  return (
    <section
      className={clsx(
        "rounded-xl border border-[color:var(--border)] bg-[color:var(--panel-strong)] p-5 shadow-[0_20px_60px_rgba(28,38,56,0.06)]",
        props.className,
      )}
    >
      {props.title || props.aside ? (
        <div className="mb-4 flex items-start justify-between gap-4">
          {props.title ? <h3 className="text-sm font-semibold uppercase tracking-[0.12em] text-[color:var(--muted)]">{props.title}</h3> : <span />}
          {props.aside}
        </div>
      ) : null}
      {props.children}
    </section>
  );
}

export function Button(
  props: PropsWithChildren<{
    onClick?: () => void;
    type?: "button" | "submit";
    variant?: "primary" | "secondary" | "danger";
    disabled?: boolean;
    className?: string;
  }>,
) {
  const variant = props.variant || "primary";
  return (
    <button
      type={props.type || "button"}
      disabled={props.disabled}
      onClick={props.onClick}
      className={clsx(
        "rounded-lg px-4 py-2 text-sm font-medium transition disabled:cursor-not-allowed disabled:opacity-50",
        variant === "primary" && "bg-[color:var(--accent)] text-white hover:brightness-110",
        variant === "secondary" && "bg-white text-[color:var(--text)] ring-1 ring-[color:var(--border)] hover:bg-[color:var(--background)]",
        variant === "danger" && "bg-[color:var(--danger)] text-white hover:brightness-110",
        props.className,
      )}
    >
      {props.children}
    </button>
  );
}

export function Input(props: InputHTMLAttributes<HTMLInputElement>) {
  return (
    <input
      {...props}
      className={clsx(
        "w-full rounded-lg border border-[color:var(--border)] bg-white px-3 py-2 text-sm text-[color:var(--text)] shadow-sm outline-none focus:border-[color:var(--accent)] focus:ring-2 focus:ring-[color:var(--accent-soft)]",
        props.className,
      )}
    />
  );
}

export function Textarea(props: TextareaHTMLAttributes<HTMLTextAreaElement>) {
  return (
    <textarea
      {...props}
      className={clsx(
        "min-h-28 w-full rounded-lg border border-[color:var(--border)] bg-white px-3 py-2 text-sm text-[color:var(--text)] shadow-sm outline-none focus:border-[color:var(--accent)] focus:ring-2 focus:ring-[color:var(--accent-soft)]",
        props.className,
      )}
    />
  );
}

export function Field(props: PropsWithChildren<{ label: string; hint?: string }>) {
  return (
    <label className="block space-y-2">
      <div className="text-sm font-medium">{props.label}</div>
      {props.children}
      {props.hint ? <div className="text-xs text-[color:var(--muted)]">{props.hint}</div> : null}
    </label>
  );
}

export function StatusBadge(props: { value: string }) {
  const tone =
    props.value === "completed"
      ? "bg-emerald-100 text-emerald-800"
      : props.value === "failed"
        ? "bg-rose-100 text-rose-700"
        : props.value === "running"
          ? "bg-sky-100 text-sky-700"
          : props.value === "blocked"
            ? "bg-amber-100 text-amber-700"
            : "bg-stone-200 text-stone-700";

  return (
    <span className={clsx("inline-flex rounded-full px-2.5 py-1 text-xs font-medium", tone)}>
      {props.value}
    </span>
  );
}

export function EmptyState(props: { title: string; body: string }) {
  return (
    <div className="rounded-xl border border-dashed border-[color:var(--border)] bg-white/70 px-6 py-8 text-center">
      <h3 className="text-base font-semibold">{props.title}</h3>
      <p className="mt-2 text-sm text-[color:var(--muted)]">{props.body}</p>
    </div>
  );
}

export function Notice(props: { tone?: "info" | "danger"; children: ReactNode }) {
  const tone = props.tone || "info";
  return (
    <div
      className={clsx(
        "rounded-xl px-4 py-3 text-sm",
        tone === "info" && "bg-[color:var(--background)] text-[color:var(--muted)]",
        tone === "danger" && "bg-rose-50 text-rose-700 ring-1 ring-rose-100",
      )}
    >
      {props.children}
    </div>
  );
}
