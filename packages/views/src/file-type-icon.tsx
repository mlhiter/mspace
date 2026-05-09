import type { ComponentType } from "react";
import CodeOutlinedIcon from "@mui/icons-material/CodeOutlined";
import CssOutlinedIcon from "@mui/icons-material/CssOutlined";
import DataObjectOutlinedIcon from "@mui/icons-material/DataObjectOutlined";
import FolderZipOutlinedIcon from "@mui/icons-material/FolderZipOutlined";
import HtmlOutlinedIcon from "@mui/icons-material/HtmlOutlined";
import ImageOutlinedIcon from "@mui/icons-material/ImageOutlined";
import Inventory2OutlinedIcon from "@mui/icons-material/Inventory2Outlined";
import JavascriptOutlinedIcon from "@mui/icons-material/JavascriptOutlined";
import KeyOutlinedIcon from "@mui/icons-material/KeyOutlined";
import LockOutlinedIcon from "@mui/icons-material/LockOutlined";
import NotesOutlinedIcon from "@mui/icons-material/NotesOutlined";
import SettingsOutlinedIcon from "@mui/icons-material/SettingsOutlined";
import StorageOutlinedIcon from "@mui/icons-material/StorageOutlined";
import TableChartOutlinedIcon from "@mui/icons-material/TableChartOutlined";
import TerminalOutlinedIcon from "@mui/icons-material/TerminalOutlined";
import { cn } from "@mspace/ui";

type MaterialFileIcon = ComponentType<{
  className?: string;
  "aria-hidden"?: boolean;
  "data-icon"?: boolean;
  fontSize?: "inherit";
}>;

type FileIconKind =
  | "archive"
  | "code"
  | "config"
  | "css"
  | "data"
  | "docker"
  | "html"
  | "image"
  | "javascript"
  | "lock"
  | "markdown"
  | "secret"
  | "shell"
  | "sql"
  | "table";

const filenameKinds = new Map<string, FileIconKind>([
  [".dockerignore", "docker"],
  [".env", "secret"],
  [".env.example", "secret"],
  [".gitignore", "config"],
  [".npmrc", "config"],
  ["dockerfile", "docker"],
  ["makefile", "shell"],
  ["package-lock.json", "lock"],
  ["pnpm-lock.yaml", "lock"],
  ["yarn.lock", "lock"],
]);

const extensionKinds = new Map<string, FileIconKind>([
  ["7z", "archive"],
  ["bash", "shell"],
  ["c", "code"],
  ["cert", "secret"],
  ["cjs", "javascript"],
  ["cpp", "code"],
  ["crt", "secret"],
  ["cs", "code"],
  ["css", "css"],
  ["csv", "table"],
  ["db", "sql"],
  ["fish", "shell"],
  ["gif", "image"],
  ["go", "code"],
  ["gz", "archive"],
  ["h", "code"],
  ["hpp", "code"],
  ["html", "html"],
  ["ico", "image"],
  ["ini", "config"],
  ["java", "code"],
  ["jpeg", "image"],
  ["jpg", "image"],
  ["js", "javascript"],
  ["json", "data"],
  ["jsonl", "data"],
  ["jsx", "javascript"],
  ["key", "secret"],
  ["kt", "code"],
  ["less", "css"],
  ["lock", "lock"],
  ["log", "markdown"],
  ["mjs", "javascript"],
  ["md", "markdown"],
  ["mdx", "markdown"],
  ["pem", "secret"],
  ["php", "code"],
  ["png", "image"],
  ["ps1", "shell"],
  ["py", "code"],
  ["rar", "archive"],
  ["rb", "code"],
  ["rs", "code"],
  ["sass", "css"],
  ["scss", "css"],
  ["sh", "shell"],
  ["sql", "sql"],
  ["sqlite", "sql"],
  ["svg", "image"],
  ["swift", "code"],
  ["tar", "archive"],
  ["tgz", "archive"],
  ["toml", "config"],
  ["ts", "code"],
  ["tsx", "code"],
  ["tsv", "table"],
  ["txt", "markdown"],
  ["webp", "image"],
  ["xls", "table"],
  ["xlsx", "table"],
  ["yaml", "config"],
  ["yml", "config"],
  ["zip", "archive"],
  ["zsh", "shell"],
]);

const iconByKind: Record<FileIconKind, MaterialFileIcon> = {
  archive: FolderZipOutlinedIcon,
  code: CodeOutlinedIcon,
  config: SettingsOutlinedIcon,
  css: CssOutlinedIcon,
  data: DataObjectOutlinedIcon,
  docker: Inventory2OutlinedIcon,
  html: HtmlOutlinedIcon,
  image: ImageOutlinedIcon,
  javascript: JavascriptOutlinedIcon,
  lock: LockOutlinedIcon,
  markdown: NotesOutlinedIcon,
  secret: KeyOutlinedIcon,
  shell: TerminalOutlinedIcon,
  sql: StorageOutlinedIcon,
  table: TableChartOutlinedIcon,
};

const colorByKind: Record<FileIconKind, string> = {
  archive: "text-[color:var(--muted-strong)]",
  code: "text-[color:var(--accent-blue)]",
  config: "text-[color:var(--muted-strong)]",
  css: "text-[color:var(--accent-blue)]",
  data: "text-[color:var(--accent-blue)]",
  docker: "text-[color:var(--accent-blue)]",
  html: "text-[color:var(--warning)]",
  image: "text-[color:var(--success)]",
  javascript: "text-[color:var(--warning)]",
  lock: "text-[color:var(--muted-strong)]",
  markdown: "text-[color:var(--muted-strong)]",
  secret: "text-[color:var(--danger)]",
  shell: "text-[color:var(--muted-strong)]",
  sql: "text-[color:var(--success)]",
  table: "text-[color:var(--success)]",
};

function stripLineSuffix(path: string) {
  return path.replace(/:\d+(?::\d+)?$/, "");
}

function filenameFromPath(path: string) {
  const cleanPath = stripLineSuffix(path).trim();
  return cleanPath.split(/[\\/]/).pop()?.toLowerCase() || cleanPath.toLowerCase();
}

function extensionFromFilename(filename: string) {
  const dotIndex = filename.lastIndexOf(".");
  if (dotIndex <= 0 || dotIndex === filename.length - 1) return "";
  return filename.slice(dotIndex + 1);
}

function fileIconKind(path: string): FileIconKind {
  const filename = filenameFromPath(path);
  return filenameKinds.get(filename) || extensionKinds.get(extensionFromFilename(filename)) || "code";
}

export function FileTypeIcon(props: { path: string; className?: string }) {
  const kind = fileIconKind(props.path);
  const Icon = iconByKind[kind];

  return (
    <Icon
      aria-hidden
      data-icon
      fontSize="inherit"
      className={cn("shrink-0 text-[15px]", colorByKind[kind], props.className)}
    />
  );
}
