import { MediaBarManager } from '../lib/mediabar';
import { normalizeKeyName } from '../lib/shortcuts';
import type { ExtensionMessage, KeyStateMessage, ResetKeysMessage } from '../lib/types';
export default defineContentScript({
  matches: ['*://*/*'],
  runAt: 'document_start',
  main() {
    function handleKeyEvent(e: KeyboardEvent) {
      if (e.repeat) return;
      const norm = normalizeKeyName(e.key);
      if (!norm) return;

      const isDown = e.type === 'keydown';
      const msg: KeyStateMessage = {
        type: 'KEY_STATE_CHANGED',
        key: norm,
        isDown,
      };
      try {
        void chrome.runtime.sendMessage(msg);
      } catch {
        // Extension context might be invalidated
      }
    }

    function handleBlur() {
      try {
        const msg: ResetKeysMessage = { type: 'RESET_KEYS' };
        void chrome.runtime.sendMessage(msg);
      } catch {
        // Extension context might be invalidated
      }
    }

    // Capture phase listeners (IDM style) to intercept keys before page handlers
    window.addEventListener('keydown', handleKeyEvent, true);
    window.addEventListener('keyup', handleKeyEvent, true);
    window.addEventListener('blur', handleBlur);

    // Initialize media floating bar manager
    const mediaBar = new MediaBarManager();
    if (document.readyState === 'loading') {
      document.addEventListener('DOMContentLoaded', () => {
        mediaBar.start();
      });
    } else {
      mediaBar.start();
    }

    // Request currently detected media resources for this tab
    try {
      chrome.runtime.sendMessage({ type: 'GET_TAB_MEDIA' }, (res) => {
        if (res && Array.isArray(res)) {
          mediaBar.setResources(res);
        }
      });
    } catch {
      //
    }

    // Listen for media update messages from background
    chrome.runtime.onMessage.addListener((msg: ExtensionMessage) => {
      if (msg && msg.type === 'GET_TAB_MEDIA') {
        //
      }
    });
  },
});
