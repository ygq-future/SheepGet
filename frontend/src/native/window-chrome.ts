import { Window } from '@wailsio/runtime';

export type ChromePlatform = 'windows' | 'darwin' | 'linux';

export type WindowControlKind = 'minimize' | 'close';

export const PLATFORM_CLASS: Record<ChromePlatform, string> = {
  windows: 'is_windows',
  darwin: 'is_mac',
  linux: 'is_linux',
};

function isChromePlatform(value: string): value is ChromePlatform {
  return value === 'windows' || value === 'darwin' || value === 'linux';
}

function chromePlatform(): ChromePlatform | null {
  if (!('_wails' in window) || typeof window._wails !== 'object' || window._wails === null) {
    return null;
  }
  if (!('environment' in window._wails) || typeof window._wails.environment !== 'object') {
    return null;
  }
  const environment = window._wails.environment;
  if (!environment || !('OS' in environment)) return null;
  const os = environment.OS;
  return typeof os === 'string' && isChromePlatform(os) ? os : null;
}

export function resolveWindowControl(
  pressed: WindowControlKind | null,
  released: WindowControlKind | null,
): WindowControlKind | null {
  return pressed !== null && pressed === released ? pressed : null;
}

export function runWindowControl(kind: WindowControlKind): void {
  const request = kind === 'minimize' ? Window.Minimise() : Window.Close();
  void request.catch((error: unknown) => {
    console.error('window control failed', kind, error);
  });
}

export function installWindowChrome(): void {
  const start = () => {
    const platform = chromePlatform();
    if (!platform) return;
    document.documentElement.classList.add(PLATFORM_CLASS[platform]);
    if (platform === 'windows') installMouseGesture();
  };

  if (chromePlatform()) start();
  else window.addEventListener('wails:runtime-config-ready', start, { once: true });
}

function installMouseGesture(): void {
  let pressed: WindowControlKind | null = null;

  window.addEventListener(
    'mousedown',
    (event) => {
      if (event.button !== 0) return;
      pressed = controlUnderPoint(event.clientX, event.clientY);
      if (!pressed) return;
      event.stopPropagation();
      event.preventDefault();
    },
    { capture: true },
  );

  window.addEventListener(
    'mouseup',
    (event) => {
      if (event.button !== 0) return;
      const started = pressed;
      pressed = null;
      const kind = resolveWindowControl(started, controlUnderPoint(event.clientX, event.clientY));
      if (kind) runWindowControl(kind);
    },
    { capture: true },
  );

  window.addEventListener(
    'click',
    (event) => {
      if (event.detail !== 0) return;
      const kind = controlUnderPoint(event.clientX, event.clientY);
      if (kind) runWindowControl(kind);
    },
    { capture: true },
  );

  window.addEventListener('blur', () => {
    pressed = null;
  });
}

function controlUnderPoint(clientX: number, clientY: number): WindowControlKind | null {
  const raw = document
    .elementFromPoint(clientX, clientY)
    ?.closest<HTMLElement>('[data-window-control]')?.dataset.windowControl;
  return raw === 'minimize' || raw === 'close' ? raw : null;
}
