import { MediaBarManager } from '../lib/mediabar';
import { inferPageTitle, type MediaResource } from '../lib/media';
import { normalizeKeyName } from '../lib/shortcuts';
import type { ExtensionMessage, KeyStateMessage, ResetKeysMessage } from '../lib/types';
export default defineContentScript({
  matches: ['*://*/*'],
  runAt: 'document_start',
  // 视频播放器常被嵌在 iframe 里（嵌入页、视频站的分帧播放、外站引用），
  // 只在主框架注入会让这类页面的悬浮条永远不出现。与 IDM 对齐，注入所有框架。
  allFrames: true,
  matchAboutBlank: true,
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

    // 上报页面真实标题与 URL：HLS 清单的 URL 末段往往是 index.m3u8，真正的视频名
    // 只存在于页面标题里（og:title / <title>）。background 据此给本标签页的资源命名。
    // 标题可能在 media script 注入后仍被 SPA 改写，这里延迟到 DOM 就绪后再报一次。
    function reportPageContext() {
      try {
        const msg: ExtensionMessage = {
          type: 'REPORT_PAGE_CONTEXT',
          pageTitle: inferPageTitle(document),
          pageUrl: location.href,
        };
        void chrome.runtime.sendMessage(msg);
      } catch {
        // Extension context might be invalidated
      }
    }
    if (document.readyState === 'loading') {
      document.addEventListener('DOMContentLoaded', reportPageContext, { once: true });
    } else {
      reportPageContext();
    }

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
