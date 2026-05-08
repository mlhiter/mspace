import { useEffect, useMemo, useState } from "react";
import type { FormEvent, ReactNode } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Bot, CheckCircle2, Circle, Clock3, Plus, Save, Settings2, SquareTerminal, X } from "lucide-react";
import { api, queryKeys, type AgentProfile, type AgentProfileInput } from "@mspace/core";
import {
  Button,
  CollectionEmptyState,
  Field,
  InlineMeta,
  Input,
  Notice,
  PageFrame,
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
  Textarea,
  cn,
} from "@mspace/ui";
import { RelativeTime } from "./time";

const emptyAgentForm: AgentProfileInput = {
  name: "",
  mention: "",
  provider: "codex",
  description: "",
  instructions: "",
  enabled: true,
};

function agentToForm(agent: AgentProfile): AgentProfileInput {
  return {
    name: agent.name,
    mention: agent.mention,
    provider: agent.provider,
    description: agent.description,
    instructions: agent.instructions,
    enabled: agent.enabled,
  };
}

function normalizeAgentForm(form: AgentProfileInput): AgentProfileInput {
  const mention = form.mention.trim();
  return {
    ...form,
    name: form.name.trim(),
    mention: mention && !mention.startsWith("@") ? `@${mention}` : mention,
    provider: form.provider || "codex",
    description: form.description.trim(),
    instructions: form.instructions.trim(),
  };
}

export function AgentsPage() {
  const queryClient = useQueryClient();
  const agentsQuery = useQuery({
    queryKey: queryKeys.agents,
    queryFn: api.listAgents,
  });
  const agents = useMemo(() => agentsQuery.data || [], [agentsQuery.data]);
  const enabledCount = agents.filter((agent) => agent.enabled).length;
  const [createOpen, setCreateOpen] = useState(false);
  const [createForm, setCreateForm] = useState<AgentProfileInput>(emptyAgentForm);
  const [settingsAgent, setSettingsAgent] = useState<AgentProfile | null>(null);
  const [settingsForm, setSettingsForm] = useState<AgentProfileInput>(emptyAgentForm);

  const createAgent = useMutation({
    mutationFn: (input: AgentProfileInput) => api.createAgent(input),
    onSuccess: async () => {
      setCreateForm(emptyAgentForm);
      setCreateOpen(false);
      await queryClient.invalidateQueries({ queryKey: queryKeys.agents });
    },
  });

  const updateAgent = useMutation({
    mutationFn: (input: AgentProfileInput) => {
      if (!settingsAgent) throw new Error("No agent selected.");
      return api.updateAgent(settingsAgent.id, input);
    },
    onSuccess: async () => {
      setSettingsAgent(null);
      setSettingsForm(emptyAgentForm);
      await queryClient.invalidateQueries({ queryKey: queryKeys.agents });
    },
  });

  function openCreateModal() {
    setCreateForm(emptyAgentForm);
    createAgent.reset();
    setCreateOpen(true);
  }

  function openSettings(agent: AgentProfile) {
    setSettingsAgent(agent);
    setSettingsForm(agentToForm(agent));
    updateAgent.reset();
  }

  function submitCreate(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    createAgent.mutate(normalizeAgentForm(createForm));
  }

  function submitSettings(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    updateAgent.mutate(normalizeAgentForm(settingsForm));
  }

  return (
    <PageFrame
      title="Agents"
      subtitle="Manage the mentionable roles that can take a turn from an issue comment."
      actions={
        <Button variant="secondary" onClick={openCreateModal}>
          <Plus data-icon />
          New agent
        </Button>
      }
    >
      {agentsQuery.isPending ? (
        <div className="rounded-[10px] bg-[color:var(--surface)] px-4 py-6 text-[13px] text-[color:var(--muted)] shadow-[inset_0_0_0_1px_var(--line)]">
          Loading agents...
        </div>
      ) : agents.length === 0 ? (
        <CollectionEmptyState
          icon={Bot}
          title="No agents yet"
          body="Create a mentionable role first. Issue comments can route a turn once an agent is enabled."
          action={
            <Button variant="secondary" onClick={openCreateModal}>
              <Plus data-icon />
              New agent
            </Button>
          }
        />
      ) : (
        <div className="rounded-[10px] bg-[color:var(--surface)] shadow-[inset_0_0_0_1px_var(--line)]">
          <div className="grid grid-cols-[minmax(190px,1.05fr)_minmax(260px,1.5fr)_150px_116px_116px] gap-4 border-b border-[color:var(--line)] px-4 py-2.5 text-[12px] font-medium text-[color:var(--muted)]">
            <span>Agent</span>
            <span>Role</span>
            <span>Provider</span>
            <span>Status</span>
            <span className="text-right">Actions</span>
          </div>
          <div className="divide-y divide-[color:var(--line)]">
            {agents.map((agent) => (
              <AgentRow key={agent.id} agent={agent} onSettings={() => openSettings(agent)} />
            ))}
          </div>
          <div className="border-t border-[color:var(--line)] px-4 py-2.5 text-[12px] leading-5 text-[color:var(--muted)]">
            {enabledCount} enabled / {agents.length} total
          </div>
        </div>
      )}

      {createOpen ? (
        <AgentModal
          title="New agent"
          description="Create a role that can be mentioned from the issue composer."
          isPending={createAgent.isPending}
          error={createAgent.error}
          form={createForm}
          submitLabel="Create agent"
          pendingLabel="Creating..."
          onClose={() => setCreateOpen(false)}
          onSubmit={submitCreate}
          onChange={setCreateForm}
        />
      ) : null}

      {settingsAgent ? (
        <AgentModal
          compact
          title="Agent settings"
          description="Tune mention, role guidance, and whether this agent can start new sessions."
          isPending={updateAgent.isPending}
          error={updateAgent.error}
          form={settingsForm}
          agent={settingsAgent}
          submitLabel="Save settings"
          pendingLabel="Saving..."
          onClose={() => setSettingsAgent(null)}
          onSubmit={submitSettings}
          onChange={setSettingsForm}
        />
      ) : null}
    </PageFrame>
  );
}

function AgentRow(props: { agent: AgentProfile; onSettings: () => void }) {
  const { agent } = props;
  return (
    <article className="grid grid-cols-[minmax(190px,1.05fr)_minmax(260px,1.5fr)_150px_116px_116px] items-center gap-4 px-4 py-3 transition-[background-color] duration-150 ease-out hover:bg-[color:var(--hover)]">
      <div className="min-w-0">
        <div className="flex min-w-0 items-center gap-2">
          <span className="grid size-8 shrink-0 place-items-center rounded-[8px] bg-[color:var(--paper)] text-[color:var(--muted)] shadow-[inset_0_0_0_1px_var(--line)]">
            <Bot data-icon />
          </span>
          <div className="min-w-0">
            <div className="flex min-w-0 items-center gap-2">
              <h3 className="truncate text-[15px] font-semibold leading-6 text-[color:var(--text)]">{agent.name}</h3>
              {agent.builtIn ? (
                <span className="shrink-0 rounded-full bg-[color:var(--block)] px-2 py-0.5 text-[11px] font-medium leading-4 text-[color:var(--muted-strong)]">
                  built-in
                </span>
              ) : null}
            </div>
            <div className="mt-0.5 text-[12px] leading-5 text-[color:var(--muted)]">{agent.mention}</div>
          </div>
        </div>
      </div>

      <div className="min-w-0">
        <div className="line-clamp-2 text-[13px] leading-5 text-[color:var(--muted)]">
          {agent.description || "No description yet."}
        </div>
        <div className="mt-1">
          <InlineMeta icon={Clock3}><RelativeTime prefix="Updated" value={agent.updatedAt} /></InlineMeta>
        </div>
      </div>

      <div className="min-w-0">
        <InlineMeta icon={SquareTerminal}>{agent.provider}</InlineMeta>
      </div>

      <div>
        <AgentStatus enabled={agent.enabled} />
      </div>

      <div className="flex justify-end">
        <Button variant="secondary" size="sm" onClick={props.onSettings}>
          <Settings2 data-icon />
          Settings
        </Button>
      </div>
    </article>
  );
}

function AgentStatus(props: { enabled: boolean }) {
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-[12px] font-medium",
        props.enabled
          ? "bg-[color:var(--success-soft)] text-[color:var(--success)]"
          : "bg-[color:var(--block)] text-[color:var(--muted-strong)]",
      )}
    >
      {props.enabled ? <CheckCircle2 data-icon /> : <Circle data-icon />}
      {props.enabled ? "enabled" : "disabled"}
    </span>
  );
}

function AgentModal(props: {
  title: string;
  description: string;
  form: AgentProfileInput;
  submitLabel: string;
  pendingLabel: string;
  onClose: () => void;
  onSubmit: (event: FormEvent<HTMLFormElement>) => void;
  onChange: (form: AgentProfileInput) => void;
  isPending: boolean;
  error?: Error | null;
  agent?: AgentProfile;
  compact?: boolean;
}) {
  return (
    <Modal title={props.title} description={props.description} onClose={props.onClose} compact={props.compact}>
      <form className="flex flex-col gap-4" onSubmit={props.onSubmit}>
        {props.error ? <Notice tone="danger">{props.error.message}</Notice> : null}
        {props.agent?.builtIn ? (
          <Notice>
            This is a default agent. You can still tune its mention, enabled state, and role instructions.
          </Notice>
        ) : null}

        <div className="grid gap-3 md:grid-cols-2">
          <Field label="Name">
            <Input
              value={props.form.name}
              onChange={(event) => props.onChange({ ...props.form, name: event.target.value })}
              placeholder="Review"
            />
          </Field>
          <Field label="Mention">
            <Input
              value={props.form.mention}
              onChange={(event) => props.onChange({ ...props.form, mention: event.target.value })}
              placeholder="@review"
            />
          </Field>
        </div>

        <div className="grid gap-3 md:grid-cols-[minmax(0,1fr)_180px]">
          <Field label="Description">
            <Input
              value={props.form.description}
              onChange={(event) => props.onChange({ ...props.form, description: event.target.value })}
              placeholder="Code review, risk checks, and merge readiness."
            />
          </Field>
          <Field label="Provider">
            <Select
              value={props.form.provider}
              onValueChange={(value) => props.onChange({ ...props.form, provider: value })}
            >
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="codex">Codex app-server</SelectItem>
              </SelectContent>
            </Select>
          </Field>
        </div>

        <Field label="Instructions" hint="Keep this as role guidance. Issue, comments, project, and session context are added separately.">
          <Textarea
            value={props.form.instructions}
            onChange={(event) => props.onChange({ ...props.form, instructions: event.target.value })}
            className="h-[220px] !min-h-[220px] leading-6"
            placeholder="Focus on code review. Identify correctness, regression, and test risks first. Keep the response concise and actionable."
          />
        </Field>

        <label className="flex items-center gap-3 rounded-[8px] bg-[color:var(--block)] px-3 py-3 text-[13px] text-[color:var(--muted-strong)] shadow-[inset_0_0_0_1px_var(--line)]">
          <input
            type="checkbox"
            checked={props.form.enabled}
            onChange={(event) => props.onChange({ ...props.form, enabled: event.target.checked })}
            className="size-4 rounded border-[color:var(--line)] accent-[color:var(--ink)]"
          />
          <span className="min-w-0">
            <span className="flex items-center gap-2 font-medium text-[color:var(--text)]">
              <SquareTerminal data-icon />
              Enabled for issue mentions
            </span>
            <span className="mt-1 block text-[12px] leading-5 text-[color:var(--muted)]">
              Disabled agents stay in history but will not appear in composer suggestions or start new sessions.
            </span>
          </span>
        </label>

        <div className="mt-1 flex justify-end gap-2">
          <Button type="button" variant="secondary" onClick={props.onClose} disabled={props.isPending}>
            Cancel
          </Button>
          <Button type="submit" disabled={props.isPending}>
            <Save data-icon />
            {props.isPending ? props.pendingLabel : props.submitLabel}
          </Button>
        </div>
      </form>
    </Modal>
  );
}

function Modal(props: { title: string; description: string; onClose: () => void; children: ReactNode; compact?: boolean }) {
  useEffect(() => {
    function handleKeyDown(event: KeyboardEvent) {
      if (event.key === "Escape") props.onClose();
    }

    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [props]);

  return (
    <div className="fixed inset-0 z-[80] grid place-items-center bg-[rgba(31,31,31,0.18)] px-5 py-8">
      <button type="button" aria-label="Close modal backdrop" className="absolute inset-0 cursor-default" onClick={props.onClose} />
      <section
        role="dialog"
        aria-modal="true"
        aria-labelledby="agent-modal-title"
        className={cn(
          "relative w-full rounded-[12px] bg-[color:var(--paper)] shadow-[0_24px_70px_rgba(0,0,0,0.18),0_0_0_1px_var(--line)]",
          props.compact
            ? "max-h-[calc(100vh-40px)] max-w-[640px] overflow-auto p-4 md:overflow-visible"
            : "max-h-[min(760px,calc(100vh-64px))] max-w-[660px] overflow-auto p-5",
        )}
      >
        <div className={cn("flex items-start justify-between gap-4", props.compact ? "mb-3" : "mb-5")}>
          <div className="min-w-0">
            <h2
              id="agent-modal-title"
              className={cn("font-semibold text-[color:var(--text)]", props.compact ? "text-[18px] leading-6" : "text-[20px] leading-7")}
            >
              {props.title}
            </h2>
            <p className={cn("mt-1 max-w-[58ch] text-[13px] text-[color:var(--muted)] text-pretty", props.compact ? "leading-5" : "leading-6")}>
              {props.description}
            </p>
          </div>
          <button
            type="button"
            aria-label="Close modal"
            className="grid size-9 shrink-0 place-items-center rounded-[7px] text-[color:var(--muted)] transition-[background-color,color,transform] duration-150 ease-out hover:bg-[color:var(--hover)] hover:text-[color:var(--text)] active:scale-95"
            onClick={props.onClose}
          >
            <X data-icon />
          </button>
        </div>
        {props.children}
      </section>
    </div>
  );
}
