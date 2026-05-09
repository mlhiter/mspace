import { useEffect } from "react";
import { EditorContent, useEditor } from "@tiptap/react";
import { StarterKit } from "@tiptap/starter-kit";
import { Markdown } from "@tiptap/markdown";
import { TaskItem } from "@tiptap/extension-task-item";
import { TaskList } from "@tiptap/extension-task-list";
import { Placeholder } from "@tiptap/extension-placeholder";

export function IssueDocumentEditor(props: {
  value: string;
  placeholder: string;
  autoFocus?: boolean;
  onChange: (value: string) => void;
}) {
  const editor = useEditor({
    immediatelyRender: false,
    extensions: [
      StarterKit,
      TaskList,
      TaskItem.configure({ nested: true }),
      Markdown.configure({
        markedOptions: {
          gfm: true,
          breaks: false,
        },
      }),
      Placeholder.configure({
        placeholder: props.placeholder,
      }),
    ],
    content: props.value,
    contentType: "markdown",
    editorProps: {
      attributes: {
        "aria-label": "Issue document",
      },
    },
    onUpdate: ({ editor: activeEditor }) => {
      props.onChange(activeEditor.getMarkdown());
    },
  });

  useEffect(() => {
    if (!props.autoFocus || !editor) return;
    window.requestAnimationFrame(() => {
      editor.commands.focus("end");
    });
  }, [editor, props.autoFocus]);

  return <EditorContent editor={editor} className="mspace-doc-editor" />;
}
