import { useEffect, useRef, useState, type ClipboardEvent, type DragEvent, type KeyboardEvent } from "react";
import { Extension, InputRule, mergeAttributes, type Editor } from "@tiptap/core";
import { Image } from "@tiptap/extension-image";
import { EditorContent, NodeViewWrapper, ReactNodeViewRenderer, useEditor, type ReactNodeViewProps } from "@tiptap/react";
import { BubbleMenu, type BubbleMenuProps } from "@tiptap/react/menus";
import { StarterKit } from "@tiptap/starter-kit";
import { Markdown } from "@tiptap/markdown";
import { TaskItem } from "@tiptap/extension-task-item";
import { TaskList } from "@tiptap/extension-task-list";
import { Placeholder } from "@tiptap/extension-placeholder";
import { Bold, Code2, Heading1, Heading2, ImageOff, ImagePlus, Italic, List, ListChecks, Loader2, Quote, type LucideIcon } from "lucide-react";
import { buildApiUrl } from "@mspace/core";

type UploadedIssueImage = {
  id: string;
  url: string;
  filename: string;
};

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

const IssueImage = Image.extend({
  addNodeView() {
    return ReactNodeViewRenderer(IssueImagePreview);
  },

  renderHTML({ HTMLAttributes }) {
    const src = attachmentDisplaySrc(String(HTMLAttributes.src || ""));
    return ["img", mergeAttributes(this.options.HTMLAttributes, HTMLAttributes, { src })];
  },
});

function IssueImagePreview(props: ReactNodeViewProps) {
  const src = attachmentDisplaySrc(String(props.node.attrs.src || ""));
  const alt = String(props.node.attrs.alt || "");
  const title = String(props.node.attrs.title || "");
  const label = issueImageLabel(alt || title || src);
  const [status, setStatus] = useState<"loading" | "loaded" | "failed">("loading");

  useEffect(() => {
    setStatus("loading");
  }, [src]);

  return (
    <NodeViewWrapper
      as="figure"
      className={[
        "mspace-doc-editor-image-frame",
        status === "loaded" && "is-loaded",
        status === "failed" && "is-failed",
        props.selected && "is-selected",
      ]
        .filter(Boolean)
        .join(" ")}
      data-drag-handle
    >
      <img
        src={src}
        alt={alt || label}
        title={title || label}
        className="mspace-doc-editor-image"
        draggable="false"
        decoding="async"
        onLoad={() => setStatus("loaded")}
        onError={() => setStatus("failed")}
      />
      {status !== "loaded" ? (
        <figcaption className="mspace-doc-editor-image-fallback">
          {status === "loading" ? <Loader2 data-icon className="mspace-doc-editor-spinner" /> : <ImageOff data-icon />}
          <span>{status === "loading" ? "Loading image" : label}</span>
        </figcaption>
      ) : null}
    </NodeViewWrapper>
  );
}

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
  onImageUpload?: (file: File) => Promise<UploadedIssueImage>;
}) {
  const fileInputRef = useRef<HTMLInputElement | null>(null);
  const [uploadingImages, setUploadingImages] = useState(0);
  const [uploadError, setUploadError] = useState("");
  const editor = useEditor({
    immediatelyRender: false,
    extensions: [
      StarterKit,
      IssueImage.configure({
        allowBase64: false,
        HTMLAttributes: {
          class: "mspace-doc-editor-image",
        },
      }),
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

  async function insertImageFiles(files: File[]) {
    if (!editor || !props.onImageUpload || files.length === 0) return;
    setUploadError("");
    setUploadingImages((count) => count + files.length);
    try {
      for (const file of files) {
        const uploaded = await props.onImageUpload(file);
        editor
          .chain()
          .focus()
          .setImage({
            src: uploaded.url,
            alt: uploaded.filename || file.name || "image",
          })
          .run();
      }
    } catch (error) {
      setUploadError(error instanceof Error ? error.message : "Image upload failed.");
    } finally {
      setUploadingImages((count) => Math.max(0, count - files.length));
    }
  }

  function handlePaste(event: ClipboardEvent<HTMLDivElement>) {
    const files = imageFilesFromDataTransfer(event.clipboardData);
    if (files.length === 0) return;
    event.preventDefault();
    void insertImageFiles(files);
  }

  function handleDrop(event: DragEvent<HTMLDivElement>) {
    const files = imageFilesFromDataTransfer(event.dataTransfer);
    if (files.length === 0) return;
    event.preventDefault();
    void insertImageFiles(files);
  }

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
  const canUploadImages = Boolean(props.onImageUpload);

  return (
    <div
      className={variant === "comment" ? "mspace-doc-editor mspace-doc-editor--comment" : "mspace-doc-editor"}
      onPasteCapture={handlePaste}
      onDrop={handleDrop}
      onDragOver={(event) => {
        if (imageFilesFromDataTransfer(event.dataTransfer).length > 0) event.preventDefault();
      }}
      onKeyDownCapture={(event) => {
        if (editor) props.onKeyDown?.(event, editor);
      }}
    >
      <EditorContent editor={editor} />
      {canUploadImages ? (
        <div className="mspace-doc-editor-attachments">
          <input
            ref={fileInputRef}
            type="file"
            accept="image/png,image/jpeg,image/gif,image/webp"
            multiple
            className="sr-only"
            onChange={(event) => {
              const files = Array.from(event.target.files || []).filter(isImageFile);
              event.target.value = "";
              void insertImageFiles(files);
            }}
          />
          <button
            type="button"
            className="mspace-doc-editor-attachment-button"
            aria-label="Upload image"
            title="Upload image"
            disabled={uploadingImages > 0}
            onMouseDown={(event) => event.preventDefault()}
            onClick={() => fileInputRef.current?.click()}
          >
            {uploadingImages > 0 ? <Loader2 data-icon className="mspace-doc-editor-spinner" /> : <ImagePlus data-icon />}
          </button>
          {uploadingImages > 0 ? <span className="mspace-doc-editor-attachment-note">Uploading...</span> : null}
          {uploadError ? <span className="mspace-doc-editor-attachment-error">{uploadError}</span> : null}
        </div>
      ) : null}
      {editor ? <IssueEditorBubbleMenu editor={editor} /> : null}
    </div>
  );
}

function attachmentDisplaySrc(src: string) {
  if (src.startsWith("/api/attachments/")) return buildApiUrl(src);
  return src;
}

function issueImageLabel(value: string) {
  const fallback = "Image attachment";
  const clean = value.trim();
  if (!clean) return fallback;
  const path = clean.split("?")[0]?.split("#")[0] || clean;
  const lastSegment = path.split("/").filter(Boolean).at(-1) || clean;
  try {
    return decodeURIComponent(lastSegment);
  } catch {
    return lastSegment;
  }
}

function isImageFile(file: File) {
  return file.type.startsWith("image/");
}

function imageFilesFromDataTransfer(dataTransfer: DataTransfer | null): File[] {
  if (!dataTransfer) return [];
  const files: File[] = [];
  Array.from(dataTransfer.items || []).forEach((item) => {
    if (item.kind !== "file" || !item.type.startsWith("image/")) return;
    const file = item.getAsFile();
    if (file) files.push(file);
  });
  if (files.length > 0) return files;
  return Array.from(dataTransfer.files || []).filter(isImageFile);
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
