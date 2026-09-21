import React from 'react';
import { createRoot } from 'react-dom/client';
import './style.css';
import App from './App';
import { FileInfoView } from './views/FileInfoView';
import { ProgressView } from './views/ProgressView';
import { initSettingsListener, useSettingsStore } from './stores/settings';

// Ensure all windows subscribe to live settings updates
initSettingsListener();
void useSettingsStore.getState().loadSettings();

// Prevent default browser context menu in all windows to maintain native desktop app feel
window.addEventListener('contextmenu', (e) => {
  e.preventDefault();
});
const container = document.getElementById('root');

const root = createRoot(container!);

const params = new URLSearchParams(window.location.search);
const windowType = params.get('window');

let ViewComponent = App;
if (windowType === 'fileinfo') {
  document.documentElement.classList.add('window-fileinfo');
  ViewComponent = FileInfoView;
} else if (windowType === 'progress') {
  document.documentElement.classList.add('window-progress');
  ViewComponent = ProgressView;
}

root.render(
  <React.StrictMode>
    <ViewComponent />
  </React.StrictMode>,
);
