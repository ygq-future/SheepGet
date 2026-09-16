import React from 'react';
import {
  FileVideo,
  FileAudio,
  FileImage,
  FileArchive,
  FileCode,
  FileText,
  FileSpreadsheet,
  Presentation,
  AppWindow,
  Package,
  Type,
  Disc,
  File,
} from 'lucide-react';

export type FileCategory =
  | 'video'
  | 'audio'
  | 'image'
  | 'archive'
  | 'code'
  | 'document'
  | 'spreadsheet'
  | 'presentation'
  | 'executable'
  | 'package'
  | 'font'
  | 'disk'
  | 'generic';

export interface FileTypeMeta {
  category: FileCategory;
  icon: React.ComponentType<{ className?: string }>;
  colorClass: string;
  bgClass: string;
}

const EXT_MAP: Record<string, FileCategory> = {
  // Video
  mp4: 'video',
  mkv: 'video',
  webm: 'video',
  avi: 'video',
  mov: 'video',
  wmv: 'video',
  flv: 'video',
  m4v: 'video',
  ts: 'video',
  m3u8: 'video',
  rmvb: 'video',

  // Audio
  mp3: 'audio',
  wav: 'audio',
  flac: 'audio',
  aac: 'audio',
  ogg: 'audio',
  m4a: 'audio',
  wma: 'audio',
  opus: 'audio',
  alac: 'audio',

  // Image
  jpg: 'image',
  jpeg: 'image',
  png: 'image',
  gif: 'image',
  webp: 'image',
  svg: 'image',
  bmp: 'image',
  ico: 'image',
  tiff: 'image',
  psd: 'image',

  // Archive
  zip: 'archive',
  rar: 'archive',
  '7z': 'archive',
  tar: 'archive',
  gz: 'archive',
  bz2: 'archive',
  xz: 'archive',
  zst: 'archive',
  tgz: 'archive',

  // Code / Data
  js: 'code',
  jsx: 'code',
  tsx: 'code',
  json: 'code',
  html: 'code',
  css: 'code',
  scss: 'code',
  py: 'code',
  go: 'code',
  rs: 'code',
  java: 'code',
  c: 'code',
  cpp: 'code',
  h: 'code',
  hpp: 'code',
  sh: 'code',
  bat: 'code',
  ps1: 'code',
  sql: 'code',
  yaml: 'code',
  yml: 'code',
  xml: 'code',

  // Documents
  pdf: 'document',
  doc: 'document',
  docx: 'document',
  txt: 'document',
  md: 'document',
  markdown: 'document',
  rtf: 'document',
  epub: 'document',

  // Spreadsheet
  xls: 'spreadsheet',
  xlsx: 'spreadsheet',
  csv: 'spreadsheet',
  numbers: 'spreadsheet',

  // Presentation
  ppt: 'presentation',
  pptx: 'presentation',
  key: 'presentation',

  // Executables
  exe: 'executable',
  msi: 'executable',
  appimage: 'executable',
  apk: 'package',
  dmg: 'disk',
  iso: 'disk',
  img: 'disk',
  pkg: 'package',
  deb: 'package',
  rpm: 'package',

  // Fonts
  ttf: 'font',
  otf: 'font',
  woff: 'font',
  woff2: 'font',
};

export function getFileExtension(filename: string): string {
  if (!filename) return '';
  const clean = filename.split(/[?#]/)[0];
  const lastDot = clean.lastIndexOf('.');
  if (lastDot <= 0 || lastDot === clean.length - 1) return '';
  return clean.slice(lastDot + 1).toLowerCase();
}

export function getFileTypeMeta(filename: string): FileTypeMeta {
  const ext = getFileExtension(filename);
  const category = EXT_MAP[ext] || 'generic';

  switch (category) {
    case 'video':
      return {
        category,
        icon: FileVideo,
        colorClass: 'text-purple-500 dark:text-purple-400',
        bgClass: 'bg-purple-500/10 border-purple-500/20',
      };
    case 'audio':
      return {
        category,
        icon: FileAudio,
        colorClass: 'text-amber-500 dark:text-amber-400',
        bgClass: 'bg-amber-500/10 border-amber-500/20',
      };
    case 'image':
      return {
        category,
        icon: FileImage,
        colorClass: 'text-sky-500 dark:text-sky-400',
        bgClass: 'bg-sky-500/10 border-sky-500/20',
      };
    case 'archive':
      return {
        category,
        icon: FileArchive,
        colorClass: 'text-yellow-600 dark:text-yellow-400',
        bgClass: 'bg-yellow-500/10 border-yellow-500/20',
      };
    case 'code':
      return {
        category,
        icon: FileCode,
        colorClass: 'text-emerald-500 dark:text-emerald-400',
        bgClass: 'bg-emerald-500/10 border-emerald-500/20',
      };
    case 'document':
      return {
        category,
        icon: FileText,
        colorClass: 'text-blue-500 dark:text-blue-400',
        bgClass: 'bg-blue-500/10 border-blue-500/20',
      };
    case 'spreadsheet':
      return {
        category,
        icon: FileSpreadsheet,
        colorClass: 'text-teal-500 dark:text-teal-400',
        bgClass: 'bg-teal-500/10 border-teal-500/20',
      };
    case 'presentation':
      return {
        category,
        icon: Presentation,
        colorClass: 'text-orange-500 dark:text-orange-400',
        bgClass: 'bg-orange-500/10 border-orange-500/20',
      };
    case 'executable':
      return {
        category,
        icon: AppWindow,
        colorClass: 'text-rose-500 dark:text-rose-400',
        bgClass: 'bg-rose-500/10 border-rose-500/20',
      };
    case 'package':
      return {
        category,
        icon: Package,
        colorClass: 'text-indigo-500 dark:text-indigo-400',
        bgClass: 'bg-indigo-500/10 border-indigo-500/20',
      };
    case 'disk':
      return {
        category,
        icon: Disc,
        colorClass: 'text-cyan-500 dark:text-cyan-400',
        bgClass: 'bg-cyan-500/10 border-cyan-500/20',
      };
    case 'font':
      return {
        category,
        icon: Type,
        colorClass: 'text-fuchsia-500 dark:text-fuchsia-400',
        bgClass: 'bg-fuchsia-500/10 border-fuchsia-500/20',
      };
    case 'generic':
    default:
      return {
        category: 'generic',
        icon: File,
        colorClass: 'text-zinc-500 dark:text-zinc-400',
        bgClass: 'bg-zinc-500/10 border-zinc-500/20',
      };
  }
}

export function isMediaFile(filename: string): boolean {
  const ext = getFileExtension(filename);
  const cat = EXT_MAP[ext];
  return cat === 'video' || cat === 'audio';
}

interface FileIconProps {
  filename: string;
  className?: string;
  iconClassName?: string;
}

export function FileTypeIcon({ filename, className, iconClassName }: FileIconProps) {
  const meta = getFileTypeMeta(filename);
  const IconComp = meta.icon;

  return (
    <div
      className={`flex shrink-0 items-center justify-center rounded-md border ${meta.bgClass} ${
        className || 'h-5.5 w-5.5'
      }`}
    >
      <IconComp className={`${meta.colorClass} ${iconClassName || 'h-3.5 w-3.5'}`} />
    </div>
  );
}
