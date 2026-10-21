import { useEffect, useState, type ReactNode } from 'react';
import { PLATFORM_CLASS, runWindowControl, type WindowControlKind } from './window-chrome';

export function WindowControls() {
  const [isWindows, setIsWindows] = useState(() => {
    if (typeof document !== 'undefined') {
      if (document.documentElement.classList.contains(PLATFORM_CLASS.windows)) return true;
      if (navigator.userAgent.includes('Windows')) return true;
    }
    return false;
  });

  useEffect(() => {
    const check = () => {
      if (
        document.documentElement.classList.contains(PLATFORM_CLASS.windows) ||
        navigator.userAgent.includes('Windows')
      ) {
        setIsWindows(true);
      }
    };
    check();
    window.addEventListener('wails:runtime-config-ready', check, { once: true });
    const observer = new MutationObserver(check);
    observer.observe(document.documentElement, { attributes: true, attributeFilter: ['class'] });
    return () => {
      window.removeEventListener('wails:runtime-config-ready', check);
      observer.disconnect();
    };
  }, []);

  if (!isWindows) return null;

  return (
    <div className="no-drag absolute inset-y-0 right-0 z-10 flex w-[var(--shell-window-controls-w,80px)] items-stretch">
      <CaptionButton label="最小化" kind="minimize">
        <line x1="1" y1="5" x2="9" y2="5" strokeLinecap="round" />
      </CaptionButton>

      <CaptionButton label="关闭" kind="close" danger>
        <line x1="1" y1="1" x2="9" y2="9" strokeLinecap="round" />
        <line x1="9" y1="1" x2="1" y2="9" strokeLinecap="round" />
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
      className={`${
        REGION_CLASS[kind]
      } flex h-full flex-1 cursor-default items-center justify-center text-[var(--text-secondary)] outline-hidden transition-colors duration-100 [--wails-draggable:no-drag] ${
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
      onPointerDown={() => {
        window.dispatchEvent(new CustomEvent('wails:titlebar:pointerdown'));
      }}
      className={`min-w-0 flex-1 self-stretch [--wails-non-client-region:caption] ${className}`}
      style={{ ['--wails-draggable' as string]: 'drag' }}
    />
  );
}
