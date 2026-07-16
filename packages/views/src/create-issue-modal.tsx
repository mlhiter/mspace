import { useEffect, useState, type FormEvent } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { MessageSquarePlus, X } from "lucide-react";
import { type CreateIssueInput } from "@mspace/core";
import { useMspaceTranslation } from "@mspace/i18n";
import {
  Button,
  Notice,
  cn,
} from "@mspace/ui";
import { IssueDocumentEditor } from "./issue-document-editor";

export function CreateIssueModal(props: {
  onClose: () => void;
  createIssue: (input: CreateIssueInput) => Promise<{ issueId: string }>;
  issueQueryKey: readonly unknown[];
  inboxQueryKey: readonly unknown[];
  projectsQueryKey: readonly unknown[];
}) {
  const { t } = useMspaceTranslation();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [prompt, setPrompt] = useState("");
  const [promptPlainText, setPromptPlainText] = useState("");
  const canCreate = prompt.trim().length > 0;

  const submitIssue = useMutation({
    mutationFn: async (input: { body: string; title: string }) => {
      const { body, title } = input;
      if (title === "") {
        throw new Error(t("createIssue.emptyError"));
      }
      return props.createIssue({ title, titleSource: "plain_text", body });
    },
    onSuccess: ({ issueId }) => {
      setPrompt("");
      setPromptPlainText("");
      void Promise.allSettled([
        queryClient.invalidateQueries({ queryKey: props.issueQueryKey }),
        queryClient.invalidateQueries({ queryKey: props.inboxQueryKey }),
        queryClient.invalidateQueries({ queryKey: props.projectsQueryKey }),
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
    const body = prompt.trim();
    if (body === "" || submitIssue.isPending) return;
    submitIssue.mutate({ body, title: draftIssueTitleFromText(promptPlainText) });
  }

  return (
    <div className="fixed inset-0 z-[80] grid place-items-center bg-[rgba(31,31,31,0.18)] px-5 py-8">
      <button type="button" aria-label={t("createIssue.closeBackdrop")} className="absolute inset-0 cursor-default" onClick={props.onClose} />
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
              {t("createIssue.eyebrow")}
            </div>
            <h2 id="create-issue-title" className="text-[20px] font-semibold leading-7 text-[color:var(--text)]">
              {t("createIssue.title")}
            </h2>
            <p className="mt-1 max-w-[58ch] text-[13px] leading-6 text-[color:var(--muted)] text-pretty">
              {t("createIssue.description")}
            </p>
          </div>
          <button
            type="button"
            aria-label={t("createIssue.closeModal")}
            className="grid size-9 shrink-0 place-items-center rounded-[7px] text-[color:var(--muted)] transition-[background-color,color,transform] duration-150 ease-out hover:bg-[color:var(--hover)] hover:text-[color:var(--text)] active:scale-95"
            onClick={props.onClose}
          >
            <X data-icon />
          </button>
        </div>

        <form className="flex flex-col" onSubmit={handleSubmit}>
          {submitIssue.error ? (
            <div className="grid gap-3 px-8 pb-4">
              <Notice tone="danger">{submitIssue.error.message}</Notice>
            </div>
          ) : null}

          <section className="mx-8 border-y border-[color:var(--line)] py-5">
            <IssueDocumentEditor
              autoFocus
              value={prompt}
              onChange={setPrompt}
              onPlainTextChange={setPromptPlainText}
              placeholder={t("createIssue.placeholder")}
            />
          </section>

          <div className="flex justify-end gap-2 border-t border-[color:var(--line)] bg-[color:var(--surface)] px-8 py-4">
            <Button type="button" variant="secondary" onClick={props.onClose} disabled={submitIssue.isPending}>
              {t("common.cancel")}
            </Button>
            <Button type="submit" disabled={!canCreate || submitIssue.isPending} className={cn(!canCreate && "opacity-60")}>
              {submitIssue.isPending ? t("createIssue.creating") : t("createIssue.submit")}
            </Button>
          </div>
        </form>
      </section>
    </div>
  );
}

function draftIssueTitleFromText(value: string): string {
  const normalized = value.replaceAll("\r\n", "\n").replaceAll("\r", "\n");
  const firstLine = normalized
    .split("\n")
    .map((line) => line.trim())
    .find(Boolean);
  if (!firstLine) return "";
  const collapsed = firstLine.split(/\s+/).join(" ");
  const runes = Array.from(collapsed);
  if (runes.length > 64) {
    return `${runes.slice(0, 64).join("")}...`;
  }
  return collapsed;
}
