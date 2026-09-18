import { normalizeKeyName } from '../lib/shortcuts';
import type { KeyStateMessage, ResetKeysMessage } from '../lib/types';

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
  },
});
