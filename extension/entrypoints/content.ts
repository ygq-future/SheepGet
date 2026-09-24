import { MediaBarManager } from '../lib/mediabar';
import { inferPageTitle, type MediaResource } from '../lib/media';
import { safeSendMessage } from '../lib/runtime';
import { KEY_MASKS, keyStateReply, normalizeKeyName, updateKeyMask } from '../lib/shortcuts';
import type {
  BackgroundMessage,
  ExtensionMessage,
  KeyStateMessage,
  KeyStateReport,
  ResetKeysMessage,
  ShortcutClickMessage,
} from '../lib/types';
export default defineContentScript({
  matches: ['*://*/*'],
  runAt: 'document_start',
  // 视频播放器常被嵌在 iframe 里（嵌入页、视频站的分帧播放、外站引用），
  // 只在主框架注入会让这类页面的悬浮条永远不出现。与 IDM 对齐，注入所有框架。
  allFrames: true,
  matchAboutBlank: true,
  main() {
    let localKeyMask = 0;

    function cleanUpKeyListeners() {
      window.removeEventListener('keydown', handleKeyEvent, true);
      window.removeEventListener('keyup', handleKeyEvent, true);
      window.removeEventListener('click', handleClickEvent, true);
      window.removeEventListener('auxclick', handleClickEvent, true);
      window.removeEventListener('blur', handleBlur);
    }

    function isEditableElement(target: EventTarget | null): boolean {
      if (!target || !(target instanceof HTMLElement)) return false;
      const tag = target.tagName.toLowerCase();
      return tag === 'input' || tag === 'textarea' || target.isContentEditable;
    }

    function handleKeyEvent(e: KeyboardEvent) {
      if (e.repeat) return;
      if (isEditableElement(e.target)) return;
      const norm = normalizeKeyName(e.key);
      if (!norm) return;

      const isDown = e.type === 'keydown';
      localKeyMask = updateKeyMask(localKeyMask, norm, isDown);
      const msg: KeyStateMessage = {
        type: 'KEY_STATE_CHANGED',
        key: norm,
        isDown,
      };
      safeSendMessage(msg, undefined, {
        onContextInvalidated: cleanUpKeyListeners,
      });
    }

    function handleClickEvent(e: MouseEvent) {
      let clickMask = localKeyMask;
      if (e.altKey) clickMask |= KEY_MASKS.Alt;
      if (e.ctrlKey) clickMask |= KEY_MASKS.Control;
      if (e.shiftKey) clickMask |= KEY_MASKS.Shift;
      if (clickMask === 0) return;

      let targetUrl: string | undefined;
      let el: Element | null = e.target instanceof Element ? e.target : null;
      while (el && el !== document.body) {
        if (el instanceof HTMLAnchorElement && el.href) {
          targetUrl = el.href;
          break;
        }
        el = el.parentElement;
      }

      const msg: ShortcutClickMessage = {
        type: 'SHORTCUT_CLICKED',
        keyMask: clickMask,
        url: targetUrl,
        timestamp: Date.now(),
      };
      safeSendMessage(msg, undefined, {
        onContextInvalidated: cleanUpKeyListeners,
      });
    }

    function handleBlur() {
      localKeyMask = 0;
      const msg: ResetKeysMessage = { type: 'RESET_KEYS' };
      safeSendMessage(msg, undefined, {
        onContextInvalidated: cleanUpKeyListeners,
      });
    }

    // Capture phase listeners (IDM style) to intercept keys and clicks before page handlers
    window.addEventListener('keydown', handleKeyEvent, true);
    window.addEventListener('keyup', handleKeyEvent, true);
    window.addEventListener('click', handleClickEvent, true);
    window.addEventListener('auxclick', handleClickEvent, true);
    window.addEventListener('blur', handleBlur);

    // 上报页面真实标题与 URL：HLS 清单的 URL 末段往往是 index.m3u8，真正的视频名
    // 只存在于页面标题里（og:title / <title>）。background 据此给本标签页的资源命名。
    // 标题可能在 media script 注入后仍被 SPA 改写，这里延迟到 DOM 就绪后再报一次。
    function reportPageContext() {
      const msg: ExtensionMessage = {
        type: 'REPORT_PAGE_CONTEXT',
        pageTitle: inferPageTitle(document),
        pageUrl: location.href,
      };
      safeSendMessage(msg);
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
    safeSendMessage<MediaResource[]>({ type: 'GET_TAB_MEDIA' }, (res) => {
      if (res && Array.isArray(res)) {
        mediaBar.setResources(res);
      }
    });

    // 之后新嗅探到的资源由 background 主动推送：播放器后加载、用户点了播放才发起
    // 媒体请求的页面，首次拉取必然为空，只能靠这条推送完成关联。
    try {
      chrome.runtime.onMessage.addListener(
        (msg: BackgroundMessage, _sender, sendResponse: (report: KeyStateReport) => void) => {
          if (msg?.type === 'TAB_MEDIA_UPDATED') {
            mediaBar.setResources(msg.resources);
            return;
          }
          if (msg?.type === 'QUERY_KEY_STATE') {
            // 后台刚被唤醒时手上没有按键状态，问的就是这个页面此刻的本地掩码；
            // 没按住键时不回应答（见 lib/shortcuts.ts 的 keyStateReply）。
            const report = keyStateReply(localKeyMask);
            if (report) sendResponse(report);
          }
        },
      );
    } catch {
      // Extension context might be invalidated
    }
  },
});
