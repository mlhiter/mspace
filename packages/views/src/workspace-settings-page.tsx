import { useEffect, useState, type ReactNode } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { CheckCircle2, GitCommit, GitPullRequest, Save, Settings2, type LucideIcon } from "lucide-react";
import { api, queryKeys, type UpdateWorkspaceSettingsInput } from "@mspace/core";
import { Button, Notice, PageFrame, Switch } from "@mspace/ui";

const defaultSettingsForm: UpdateWorkspaceSettingsInput = {
  autoCreateDraftPr: false,
};

export function WorkspaceSettingsPage() {
  const queryClient = useQueryClient();
  const settingsQuery = useQuery({
    queryKey: queryKeys.workspaceSettings,
    queryFn: api.getWorkspaceSettings,
  });
  const [form, setForm] = useState<UpdateWorkspaceSettingsInput>(defaultSettingsForm);

  useEffect(() => {
    if (!settingsQuery.data) return;
    setForm({
      autoCreateDraftPr: settingsQuery.data.autoCreateDraftPr,
    });
  }, [settingsQuery.data]);

  const saveSettings = useMutation({
    mutationFn: api.updateWorkspaceSettings,
    onSuccess: async (settings) => {
      setForm({ autoCreateDraftPr: settings.autoCreateDraftPr });
      await queryClient.invalidateQueries({ queryKey: queryKeys.workspaceSettings });
    },
  });

  const isDirty = Boolean(settingsQuery.data && form.autoCreateDraftPr !== settingsQuery.data.autoCreateDraftPr);

  return (
    <PageFrame
      title="Workspace settings"
      subtitle="Runtime policies for local agent sessions and issue delivery."
      actions={
        isDirty || saveSettings.isPending ? (
          <Button
            type="button"
            disabled={!isDirty || saveSettings.isPending}
            onClick={() => saveSettings.mutate(form)}
          >
            <Save data-icon />
            {saveSettings.isPending ? "Saving" : "Save"}
          </Button>
        ) : settingsQuery.data ? (
          <span className="inline-flex h-9 items-center gap-1.5 rounded-[7px] px-2.5 text-[12px] font-medium leading-5 text-[color:var(--muted)]">
            <CheckCircle2 data-icon className="size-4 text-[color:var(--success)]" />
            Saved
          </span>
        ) : null
      }
    >
      <div className="grid max-w-[900px] gap-6">
        {settingsQuery.error ? <Notice tone="danger">{settingsQuery.error.message}</Notice> : null}
        {saveSettings.error ? <Notice tone="danger">{saveSettings.error.message}</Notice> : null}

        <SettingsSection
          title="Automation"
          description="These policies run after an agent source session finishes."
          meta={settingsQuery.isFetching ? "Refreshing" : "Workspace"}
        >
          <SettingsRow
            icon={GitCommit}
            title="Commit capture"
            description="Always capture source work as a commit so review, evidence, deploy selection, and PR handoff share one stable SHA."
            control={<StatusPill>Always on</StatusPill>}
          />
          <SettingsRow
            icon={GitPullRequest}
            title="Draft pull requests"
            description="Create or refresh one issue-level draft PR through local git, gh, and gitleaks after a source commit is captured."
            control={
              <Switch
                checked={form.autoCreateDraftPr}
                disabled={!settingsQuery.data || saveSettings.isPending}
                aria-label="Auto draft PR"
                onCheckedChange={(checked) => setForm({ autoCreateDraftPr: checked })}
              />
            }
          />
        </SettingsSection>

        <SettingsSection
          title="GitHub identity"
          description="PR automation uses the same local identity as manual handoff actions."
        >
          <SettingsRow
            icon={Settings2}
            title="Local GitHub CLI"
            description="The MVP uses the signed-in gh identity on this machine. Server-owned GitHub App automation remains a productized-stage boundary."
            control={<StatusPill>Local</StatusPill>}
          />
        </SettingsSection>
      </div>
    </PageFrame>
  );
}

function SettingsSection(props: {
  title: string;
  description: string;
  meta?: string;
  children: ReactNode;
}) {
  return (
    <section className="grid gap-2">
      <div className="flex min-w-0 items-end justify-between gap-4 px-0.5">
        <div className="min-w-0">
          <h2 className="text-[14px] font-semibold leading-5 text-[color:var(--text)]">{props.title}</h2>
          <p className="mt-1 max-w-[62ch] text-[12px] leading-5 text-[color:var(--muted)] text-pretty">{props.description}</p>
        </div>
        {props.meta ? <span className="shrink-0 text-[12px] leading-5 text-[color:var(--faint)]">{props.meta}</span> : null}
      </div>
      <div className="overflow-hidden rounded-[10px] bg-[color:var(--surface)] shadow-[inset_0_0_0_1px_var(--line)]">
        {props.children}
      </div>
    </section>
  );
}

function SettingsRow(props: {
  icon: LucideIcon;
  title: string;
  description: string;
  control: ReactNode;
}) {
  const Icon = props.icon;
  return (
    <div className="grid min-w-0 gap-3 border-b border-[color:var(--line)] px-4 py-3.5 last:border-b-0 md:grid-cols-[32px_minmax(0,1fr)_auto] md:items-center">
      <span className="grid size-8 place-items-center rounded-[8px] bg-[color:var(--block)] text-[color:var(--muted)] shadow-[inset_0_0_0_1px_var(--line)]">
        <Icon data-icon />
      </span>
      <span className="min-w-0">
        <span className="block text-[13px] font-medium leading-5 text-[color:var(--text)]">{props.title}</span>
        <span className="mt-1 block max-w-[68ch] text-[12px] leading-5 text-[color:var(--muted)] text-pretty">{props.description}</span>
      </span>
      <div className="justify-self-start md:justify-self-end">{props.control}</div>
    </div>
  );
}

function StatusPill(props: { children: ReactNode }) {
  return (
    <span className="inline-flex h-7 items-center rounded-full bg-[color:var(--block)] px-2.5 text-[12px] font-medium leading-5 text-[color:var(--muted-strong)] shadow-[inset_0_0_0_1px_var(--line)]">
      {props.children}
    </span>
  );
}
