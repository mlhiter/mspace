import { useEffect, useState, type FormEvent } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { MessageSquarePlus, X } from "lucide-react";
import { api, queryKeys } from "@mspace/core";
import {
  Button,
  Notice,
  cn,
} from "@mspace/ui";
import { IssueDocumentEditor } from "./issue-document-editor";

export function CreateIssueModal(props: {
  onClose: () => void;
}) {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [prompt, setPrompt] = useState("");
  const [attachmentIds, setAttachmentIds] = useState<string[]>([]);
  const canCreate = prompt.trim().length > 0;

  const createIssue = useMutation({
    mutationFn: api.createIssue,
    onSuccess: async ({ issueId }) => {
      setPrompt("");
      setAttachmentIds([]);
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: queryKeys.issues }),
        queryClient.invalidateQueries({ queryKey: queryKeys.inbox }),
        queryClient.invalidateQueries({ queryKey: queryKeys.projects }),
      ]);
      props.onClose();
      void navigate({ to: "/issues/$issueId", params: { issueId } });
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
      prompt,
      attachmentIds: attachmentIdsReferencedBy(prompt, attachmentIds),
    });
  }

  async function uploadIssueImage(file: File) {
    const attachment = await api.uploadAttachment(file);
    setAttachmentIds((current) => current.includes(attachment.id) ? current : [...current, attachment.id]);
    return {
      id: attachment.id,
      url: attachment.url,
      filename: attachment.filename,
    };
  }

  return (
    <div className="fixed inset-0 z-[80] grid place-items-center bg-[rgba(31,31,31,0.18)] px-5 py-8">
      <button type="button" aria-label="Close modal backdrop" className="absolute inset-0 cursor-default" onClick={props.onClose} />
      <section
        role="dialog"
        aria-modal="true"
        aria-labelledby="create-issue-title"
        className="relative w-full max-w-[720px] overflow-hidden rounded-[12px] bg-[color:var(--paper)] shadow-[0_24px_70px_rgba(0,0,0,0.18),0_0_0_1px_var(--line)]"
      >
        <div className="flex items-start justify-between gap-4 px-8 pb-4 pt-7">
          <div className="min-w-0">
            <div className="mb-2 flex items-center gap-2 text-[12px] text-[color:var(--muted)]">
              <MessageSquarePlus data-icon />
              Issue
            </div>
            <h2 id="create-issue-title" className="text-[20px] font-semibold leading-7 text-[color:var(--text)]">
              New issue
            </h2>
            <p className="mt-1 max-w-[58ch] text-[13px] leading-6 text-[color:var(--muted)] text-pretty">
              Describe the work in one note. mspace will route it to the best matching project.
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

        <form className="flex flex-col" onSubmit={handleSubmit}>
          {createIssue.error ? (
            <div className="grid gap-3 px-8 pb-4">
              <Notice tone="danger">{createIssue.error.message}</Notice>
            </div>
          ) : null}

          <section className="mx-8 border-y border-[color:var(--line)] py-5">
            <IssueDocumentEditor
              autoFocus
              value={prompt}
              onChange={setPrompt}
              onImageUpload={uploadIssueImage}
              placeholder={"Write the issue...\n\n- [ ] Add the first task\n- [ ] Add the next task"}
            />
          </section>

          <div className="flex justify-end gap-2 border-t border-[color:var(--line)] bg-[color:var(--surface)] px-8 py-4">
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

function attachmentIdsReferencedBy(markdown: string, attachmentIds: string[]) {
  return attachmentIds.filter((id) => markdown.includes(`/api/attachments/${id}`));
}
