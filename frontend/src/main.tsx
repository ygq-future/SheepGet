import type { ComponentType } from 'react';
import React, { Suspense, lazy } from 'react';
import { createRoot } from 'react-dom/client';
import './style.css';
import { installWindowChrome } from './native/window-chrome';
import { initSettingsListener, useSettingsStore } from './stores/settings';

// Ensure all windows subscribe to live settings updates
initSettingsListener();
void useSettingsStore.getState().loadSettings();

const params = new URLSearchParams(window.location.search);
const windowType = params.get('window');

if (windowType === null) installWindowChrome();

// Prevent default browser context menu in all windows to maintain native desktop app feel
window.addEventListener('contextmenu', (e) => {
  e.preventDefault();
});
const container = document.getElementById('root');

const root = createRoot(container!);

let ViewComponent: ComponentType;
if (windowType === 'fileinfo') {
  document.documentElement.classList.add('window-fileinfo');
  ViewComponent = lazy(() =>
    import('./views/FileInfoView').then((m) => ({ default: m.FileInfoView })),
  );
} else if (windowType === 'progress') {
  document.documentElement.classList.add('window-progress');
  ViewComponent = lazy(() =>
    import('./views/ProgressView').then((m) => ({ default: m.ProgressView })),
  );
} else {
  ViewComponent = lazy(() => import('./App'));
}

root.render(
  <React.StrictMode>
    <Suspense fallback={null}>
      <ViewComponent />
    </Suspense>
  </React.StrictMode>,
);
