import { useEffect, type KeyboardEvent } from "react";
import { Extension, InputRule, type Editor } from "@tiptap/core";
import { EditorContent, useEditor } from "@tiptap/react";
import { BubbleMenu, type BubbleMenuProps } from "@tiptap/react/menus";
import { StarterKit } from "@tiptap/starter-kit";
import { Markdown } from "@tiptap/markdown";
import { TaskItem } from "@tiptap/extension-task-item";
import { TaskList } from "@tiptap/extension-task-list";
import { Placeholder } from "@tiptap/extension-placeholder";
import { Bold, Code2, Heading1, Heading2, Italic, List, ListChecks, Quote, type LucideIcon } from "lucide-react";

const MarkdownChecklistInput = Extension.create({
  name: "markdownChecklistInput",

  addInputRules() {
    return [
      new InputRule({
        find: /^\s*[-*]\s+\[([ xX])\]\s$/,
        handler: ({ range, match, chain }) => {
          chain()
            .deleteRange(range)
            .updateAttributes("taskItem", { checked: match[1]?.toLowerCase() === "x" })
            .run();
        },
      }),
      new InputRule({
        find: /^\s*\[([ xX])\]\s$/,
        handler: ({ range, match, chain }) => {
          chain()
            .deleteRange(range)
            .toggleTaskList()
            .updateAttributes("taskItem", { checked: match[1]?.toLowerCase() === "x" })
            .run();
        },
      }),
    ];
  },
});

const shouldShowBubbleMenu: NonNullable<BubbleMenuProps["shouldShow"]> = ({ editor, state, from, to }) => {
  if (!editor.isEditable || state.selection.empty) return false;
  return state.doc.textBetween(from, to).trim().length > 0;
};

export function IssueDocumentEditor(props: {
  value: string;
  placeholder: string;
  autoFocus?: boolean;
  variant?: "document" | "comment";
  ariaLabel?: string;
  onReady?: (editor: Editor | null) => void;
  onChange: (value: string) => void;
  onEditorStateChange?: (editor: Editor) => void;
  onFocus?: (editor: Editor) => void;
  onBlur?: (editor: Editor) => void;
  onKeyDown?: (event: KeyboardEvent<HTMLDivElement>, editor: Editor) => void;
}) {
  const editor = useEditor({
    immediatelyRender: false,
    extensions: [
      StarterKit,
      TaskList,
      TaskItem.configure({ nested: true }),
      MarkdownChecklistInput,
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
        "aria-label": props.ariaLabel || "Issue document",
      },
    },
    onUpdate: ({ editor: activeEditor }) => {
      props.onChange(activeEditor.getMarkdown());
      props.onEditorStateChange?.(activeEditor);
    },
    onSelectionUpdate: ({ editor: activeEditor }) => {
      props.onEditorStateChange?.(activeEditor);
    },
    onFocus: ({ editor: activeEditor }) => {
      props.onFocus?.(activeEditor);
      props.onEditorStateChange?.(activeEditor);
    },
    onBlur: ({ editor: activeEditor }) => {
      props.onBlur?.(activeEditor);
    },
  });

  useEffect(() => {
    props.onReady?.(editor);
    return () => props.onReady?.(null);
  }, [editor, props.onReady]);

  useEffect(() => {
    if (!editor || props.value.trim() !== "") return;
    if (editor.getMarkdown().trim() !== "") {
      editor.commands.clearContent(false);
    }
  }, [editor, props.value]);

  useEffect(() => {
    if (!props.autoFocus || !editor) return;
    window.requestAnimationFrame(() => {
      editor.commands.focus("end");
    });
  }, [editor, props.autoFocus]);

  const variant = props.variant || "document";

  return (
    <div
      className={variant === "comment" ? "mspace-doc-editor mspace-doc-editor--comment" : "mspace-doc-editor"}
      onKeyDownCapture={(event) => {
        if (editor) props.onKeyDown?.(event, editor);
      }}
    >
      <EditorContent editor={editor} />
      {editor ? <IssueEditorBubbleMenu editor={editor} /> : null}
    </div>
  );
}

function IssueEditorBubbleMenu(props: { editor: Editor }) {
  const { editor } = props;

  return (
    <BubbleMenu
      editor={editor}
      updateDelay={80}
      appendTo={() => document.body}
      options={{
        placement: "top",
        offset: 8,
        flip: true,
        shift: { padding: 8 },
        inline: true,
      }}
      shouldShow={shouldShowBubbleMenu}
      className="mspace-editor-bubble"
    >
      <BubbleButton
        label="Bold"
        icon={Bold}
        active={editor.isActive("bold")}
        onClick={() => editor.chain().focus().toggleBold().run()}
      />
      <BubbleButton
        label="Italic"
        icon={Italic}
        active={editor.isActive("italic")}
        onClick={() => editor.chain().focus().toggleItalic().run()}
      />
      <BubbleButton
        label="Code"
        icon={Code2}
        active={editor.isActive("code")}
        onClick={() => editor.chain().focus().toggleCode().run()}
      />
      <span className="mspace-editor-bubble-separator" />
      <BubbleButton
        label="Heading 1"
        icon={Heading1}
        active={editor.isActive("heading", { level: 1 })}
        onClick={() => editor.chain().focus().toggleHeading({ level: 1 }).run()}
      />
      <BubbleButton
        label="Heading 2"
        icon={Heading2}
        active={editor.isActive("heading", { level: 2 })}
        onClick={() => editor.chain().focus().toggleHeading({ level: 2 }).run()}
      />
      <BubbleButton
        label="Quote"
        icon={Quote}
        active={editor.isActive("blockquote")}
        onClick={() => editor.chain().focus().toggleBlockquote().run()}
      />
      <span className="mspace-editor-bubble-separator" />
      <BubbleButton
        label="Bullet list"
        icon={List}
        active={editor.isActive("bulletList")}
        onClick={() => editor.chain().focus().toggleBulletList().run()}
      />
      <BubbleButton
        label="Task list"
        icon={ListChecks}
        active={editor.isActive("taskList")}
        onClick={() => editor.chain().focus().toggleTaskList().run()}
      />
    </BubbleMenu>
  );
}

function BubbleButton(props: {
  label: string;
  icon: LucideIcon;
  active: boolean;
  onClick: () => void;
}) {
  const Icon = props.icon;

  return (
    <button
      type="button"
      className={props.active ? "mspace-editor-bubble-button is-active" : "mspace-editor-bubble-button"}
      aria-label={props.label}
      title={props.label}
      onMouseDown={(event) => event.preventDefault()}
      onClick={props.onClick}
    >
      <Icon data-icon />
    </button>
  );
}
