import materialIconManifest from "material-icon-theme/dist/material-icons.json";
import type { Manifest } from "material-icon-theme";
import { cn } from "@mspace/ui";

const manifest = materialIconManifest as Manifest;

const baseFileNameIcons = toLookupMap(manifest.fileNames);
const lightFileNameIcons = toLookupMap(manifest.light?.fileNames);
const baseFileExtensionIcons = toLookupMap(manifest.fileExtensions);
const lightFileExtensionIcons = toLookupMap(manifest.light?.fileExtensions);
const baseFolderNameIcons = toLookupMap(manifest.folderNames);
const lightFolderNameIcons = toLookupMap(manifest.light?.folderNames);
const iconUrlCache = new Map<string, string>();

function toLookupMap(record?: Record<string, string>) {
  return new Map(Object.entries(record || {}).map(([key, value]) => [normalizeLookupValue(key), value]));
}

function normalizeLookupValue(value: string) {
  return value.trim().replace(/\\/g, "/").replace(/\/+/g, "/").toLowerCase();
}

function stripLineSuffix(path: string) {
  return path.replace(/:\d+(?::\d+)?$/, "");
}

function cleanPath(path: string) {
  return normalizeLookupValue(stripLineSuffix(path));
}

function isDirectoryPath(path: string) {
  return /[\\/]$/.test(stripLineSuffix(path).trim());
}

function pathLookupCandidates(path: string) {
  const parts = cleanPath(path).split("/").filter(Boolean);
  return parts.map((_, index) => parts.slice(index).join("/"));
}

function filenameFromPath(path: string) {
  const candidates = pathLookupCandidates(path);
  return candidates.at(-1) || cleanPath(path);
}

function extensionCandidates(filename: string) {
  const parts = filename.split(".");
  if (parts.length < 2) return [];

  return parts
    .slice(1)
    .map((_, index) => parts.slice(index + 1).join("."))
    .filter(Boolean);
}

function iconNameFromFileName(path: string) {
  for (const candidate of pathLookupCandidates(path)) {
    const iconName = lightFileNameIcons.get(candidate) || baseFileNameIcons.get(candidate);
    if (iconName) return iconName;
  }

  for (const extension of extensionCandidates(filenameFromPath(path))) {
    const iconName = lightFileExtensionIcons.get(extension) || baseFileExtensionIcons.get(extension);
    if (iconName) return iconName;
  }

  return manifest.file || "file";
}

function iconNameFromFolderName(path: string) {
  for (const candidate of pathLookupCandidates(path)) {
    const iconName = lightFolderNameIcons.get(candidate) || baseFolderNameIcons.get(candidate);
    if (iconName) return iconName;
  }

  return manifest.folder || "folder";
}

function iconFileName(iconName: string) {
  const iconPath = manifest.iconDefinitions?.[iconName]?.iconPath;
  return iconPath?.split("/").pop() || `${iconName}.svg`;
}

function materialIconUrl(iconName: string) {
  const fileName = iconFileName(iconName);
  const cached = iconUrlCache.get(fileName);
  if (cached) return cached;

  const url = new URL(`../node_modules/material-icon-theme/icons/${fileName}`, import.meta.url).href;
  iconUrlCache.set(fileName, url);
  return url;
}

export function FileTypeIcon(props: { path: string; className?: string }) {
  const iconName = isDirectoryPath(props.path) ? iconNameFromFolderName(props.path) : iconNameFromFileName(props.path);

  return (
    <img
      aria-hidden
      data-icon
      src={materialIconUrl(iconName)}
      alt=""
      draggable={false}
      className={cn("size-4 shrink-0 object-contain", props.className)}
    />
  );
}
