import { useEffect, useState, type FormEvent } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "react-router-dom";
import { GitBranch, MessageSquarePlus, X } from "lucide-react";
import { api, queryKeys, type Project } from "@mspace/core";
import { Button, Field, Notice, Select, Textarea, cn } from "@mspace/ui";

export function CreateIssueModal(props: {
  projects: Project[];
  onClose: () => void;
}) {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [projectId, setProjectId] = useState("");
  const [prompt, setPrompt] = useState("");
  const canCreate = prompt.trim().length > 0 && props.projects.length > 0;

  const createIssue = useMutation({
    mutationFn: api.createIssue,
    onSuccess: async ({ issueId }) => {
      setPrompt("");
      setProjectId("");
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: queryKeys.issues }),
        queryClient.invalidateQueries({ queryKey: queryKeys.inbox }),
        queryClient.invalidateQueries({ queryKey: queryKeys.projects }),
      ]);
      props.onClose();
      void navigate(`/issues/${issueId}`);
    },
  });

  useEffect(() => {
    function handleKeyDown(event: KeyboardEvent) {
      if (event.key === "Escape") props.onClose();
    }

    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [props]);

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!canCreate) return;

    createIssue.mutate({
      projectId: projectId || undefined,
      prompt,
    });
  }

  return (
    <div className="fixed inset-0 z-[80] grid place-items-center bg-[rgba(31,31,31,0.18)] px-5 py-8">
      <button type="button" aria-label="Close modal backdrop" className="absolute inset-0 cursor-default" onClick={props.onClose} />
      <section
        role="dialog"
        aria-modal="true"
        aria-labelledby="create-issue-title"
        className="relative w-full max-w-[620px] rounded-[12px] bg-[color:var(--paper)] p-5 shadow-[0_24px_70px_rgba(0,0,0,0.18),0_0_0_1px_var(--line)]"
      >
        <div className="mb-5 flex items-start justify-between gap-4">
          <div className="min-w-0">
            <div className="mb-2 flex items-center gap-2 text-[12px] text-[color:var(--muted)]">
              <MessageSquarePlus data-icon />
              Issue
            </div>
            <h2 id="create-issue-title" className="text-[20px] font-semibold leading-7 text-[color:var(--text)]">
              New issue
            </h2>
            <p className="mt-1 max-w-[58ch] text-[13px] leading-6 text-[color:var(--muted)] text-pretty">
              Describe the work in one note. mspace can infer the project, or you can pin one explicitly.
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

        <form className="flex flex-col gap-4" onSubmit={handleSubmit}>
          {createIssue.error ? <Notice tone="danger">{createIssue.error.message}</Notice> : null}
          {props.projects.length === 0 ? (
            <Notice>
              Project selection is optional, but mspace still needs at least one project before it can route an issue.
            </Notice>
          ) : null}

          <Field label="Issue note">
            <Textarea
              autoFocus
              className="min-h-[150px]"
              value={prompt}
              onChange={(event) => setPrompt(event.target.value)}
              placeholder="mspace inbox should show review updates only, not inline issue creation"
            />
          </Field>

          <Field label="Project" hint="Optional. Leave this on auto when the issue text names a project or repository.">
            <div className="relative">
              <GitBranch data-icon className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-[color:var(--muted)]" />
              <Select className="pl-9" value={projectId} onChange={(event) => setProjectId(event.target.value)}>
                <option value="">Let agent infer</option>
                {props.projects.map((project) => (
                  <option key={project.id} value={project.id}>
                    {project.name}
                  </option>
                ))}
              </Select>
            </div>
          </Field>

          <div className="flex justify-end gap-2">
            <Button type="button" variant="secondary" onClick={props.onClose} disabled={createIssue.isPending}>
              Cancel
            </Button>
            <Button type="submit" disabled={!canCreate || createIssue.isPending} className={cn(!canCreate && "opacity-60")}>
              {createIssue.isPending ? "Creating..." : "Create issue"}
            </Button>
          </div>
        </form>
      </section>
    </div>
  );
}
