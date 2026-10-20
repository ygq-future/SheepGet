import type { ReactNode } from 'react';
import { PLATFORM_CLASS, runWindowControl, type WindowControlKind } from './window-chrome';

export function WindowControls() {
  if (!document.documentElement.classList.contains(PLATFORM_CLASS.windows)) return null;

  return (
    <div className="no-drag absolute inset-y-0 right-0 z-10 flex w-[var(--shell-window-controls-w,80px)] items-stretch">
      <CaptionButton label="最小化" kind="minimize">
        <path d="M0 5h10" />
      </CaptionButton>

      <CaptionButton label="关闭" kind="close" danger>
        <path d="M0.5 0.5l9 9M9.5 0.5l-9 9" />
      </CaptionButton>
    </div>
  );
}

const REGION_CLASS: Record<WindowControlKind, string> = {
  minimize: '[--wails-non-client-region:minimize]',
  close: '[--wails-non-client-region:close]',
};

function CaptionButton({
  label,
  kind,
  danger = false,
  children,
}: {
  label: string;
  kind: WindowControlKind;
  danger?: boolean;
  children: ReactNode;
}) {
  return (
    <button
      type="button"
      aria-label={label}
      title={label}
      data-window-control={kind}
      onClick={(event) => {
        if (event.detail === 0) runWindowControl(kind);
      }}
      className={`${REGION_CLASS[kind]} flex h-full flex-1 cursor-default items-center justify-center text-[var(--text-secondary)] outline-hidden transition-colors duration-100 [--wails-draggable:no-drag] ${
        danger
          ? 'hover:bg-[#e81123] hover:text-white active:bg-[#c50f1f]'
          : 'hover:bg-[var(--shell-control-hover)]'
      }`}
    >
      <svg
        width="10"
        height="10"
        viewBox="0 0 10 10"
        fill="none"
        stroke="currentColor"
        strokeWidth="1"
        shapeRendering="crispEdges"
        aria-hidden
        className="pointer-events-none shrink-0"
      >
        {children}
      </svg>
    </button>
  );
}

export function TitleBarDragRegion({ className = '' }: { className?: string }) {
  return (
    <div
      className={`min-w-0 flex-1 self-stretch [--wails-non-client-region:caption] ${className}`}
      style={{ ['--wails-draggable' as string]: 'drag' }}
    />
  );
}
