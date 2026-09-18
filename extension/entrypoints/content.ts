import { MediaBarManager } from '../lib/mediabar';
import type { MediaResource } from '../lib/media';
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
      chrome.runtime.sendMessage({ type: 'GET_TAB_MEDIA' }, (res: MediaResource[] | undefined) => {
        if (res && Array.isArray(res)) {
          mediaBar.setResources(res);
        }
      });
    } catch {
      // Extension context might be invalidated
    }

    // 之后新嗅探到的资源由 background 主动推送：播放器后加载、用户点了播放才发起
    // 媒体请求的页面，首次拉取必然为空，只能靠这条推送完成关联。
    chrome.runtime.onMessage.addListener((msg: ExtensionMessage) => {
      if (msg?.type === 'TAB_MEDIA_UPDATED') {
        mediaBar.setResources(msg.resources);
      }
    });
  },
});
