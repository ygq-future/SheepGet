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

const container = document.getElementById('root');

const root = createRoot(container!);

const params = new URLSearchParams(window.location.search);
const windowType = params.get('window');

let ViewComponent = App;
if (windowType === 'fileinfo') {
  document.documentElement.classList.add('window-fileinfo');
  ViewComponent = FileInfoView;
} else if (windowType === 'progress') {
  ViewComponent = ProgressView;
}

root.render(
  <React.StrictMode>
    <ViewComponent />
  </React.StrictMode>,
);
