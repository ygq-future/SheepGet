import { useEffect, useState, type ReactNode } from 'react';
import { PLATFORM_CLASS, runWindowControl, type WindowControlKind } from './window-chrome';

export function WindowControls() {
  const [maximised, setMaximised] = useState(readMaximised);

  useEffect(() => {
    const sync = () => setMaximised(readMaximised());
    window.addEventListener('resize', sync);
    return () => window.removeEventListener('resize', sync);
  }, []);

  if (!document.documentElement.classList.contains(PLATFORM_CLASS.windows)) return null;

  return (
    <div className="no-drag absolute inset-y-0 right-0 z-10 flex w-[var(--shell-window-controls-w,138px)] items-stretch">
      <CaptionButton label="最小化" kind="minimize">
        <path d="M0 5h10" />
      </CaptionButton>

      <CaptionButton label={maximised ? '还原' : '最大化'} kind="maximize">
        {maximised ? (
          <>
            <path d="M2.5 2.5V0.5h7v7h-2" />
            <rect x="0.5" y="2.5" width="7" height="7" />
          </>
        ) : (
          <rect x="0.5" y="0.5" width="9" height="9" />
        )}
      </CaptionButton>

      <CaptionButton label="关闭" kind="close" danger>
        <path d="M0.5 0.5l9 9M9.5 0.5l-9 9" />
      </CaptionButton>
    </div>
  );
}

const REGION_CLASS: Record<WindowControlKind, string> = {
  minimize: '[--wails-non-client-region:minimize]',
  maximize: '[--wails-non-client-region:maximize]',
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

function readMaximised(): boolean {
  return window.innerWidth >= screen.availWidth - 2 && window.innerHeight >= screen.availHeight - 2;
}

export function TitleBarDragRegion({ className = '' }: { className?: string }) {
  return (
    <div
      className={`min-w-0 flex-1 self-stretch [--wails-non-client-region:caption] ${className}`}
      style={{ ['--wails-draggable' as string]: 'drag' }}
    />
  );
}
