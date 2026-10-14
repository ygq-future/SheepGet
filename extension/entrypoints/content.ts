import { MediaBarManager, extractMediaUrls } from '../lib/mediabar';
import { inferPageTitle, type MediaResource } from '../lib/media';
import { safeSendMessage } from '../lib/runtime';
import { KEY_MASKS, keyStateReply, normalizeKeyName, updateKeyMask } from '../lib/shortcuts';
import { decideTakeover } from '../lib/rules';
import { DEFAULT_TAKEOVER_CONFIG, STORAGE_KEYS } from '../lib/storage';
import type {
  BackgroundMessage,
  DirectClickHandoverMessage,
  ExtensionMessage,
  HandoverResponse,
  KeyStateMessage,
  KeyStateReport,
  ResetKeysMessage,
  ReportDomMediaMessage,
  ShortcutClickMessage,
  TakeoverConfigSync,
} from '../lib/types';

declare global {
  interface Window {
    __SHEEPGET_CONTENT_CLEANUP__?: () => void;
  }
}

export default defineContentScript({
  matches: ['*://*/*'],
  runAt: 'document_start',
  // 视频播放器常被嵌在 iframe 里（嵌入页、视频站的分帧播放、外站引用），
  // 只在主框架注入会让这类页面的悬浮条永远不出现。与 IDM 对齐，注入所有框架。
  allFrames: true,
  matchAboutBlank: true,
  main() {
    // 1. 若当前页面已有旧版本 content script 注册的清理函数，先执行旧实例清理，
    // 销毁旧 DOM 悬浮条与监听器，实现无缝热重注入平滑交接。
    if (typeof window.__SHEEPGET_CONTENT_CLEANUP__ === 'function') {
      try {
        window.__SHEEPGET_CONTENT_CLEANUP__();
      } catch {
        // Ignore cleanup error
      }
    }

    // 2. 物理强力清除：若页面上遗留有旧版本脚本创建的孤儿悬浮条 DOM 节点（例如从尚未支持
    // __SHEEPGET_CONTENT_CLEANUP__ 的老版本升级热注入），强制将其全部移除，杜绝失效的僵尸 DOM 遮挡新 UI。
    if (typeof document !== 'undefined') {
      try {
        document.querySelectorAll('.sheepget-mediabar-host').forEach((el) => {
          el.remove();
        });
      } catch {
        // Ignore DOM error
      }
    }

    const abortController = new AbortController();
    const { signal } = abortController;

    let localKeyMask = 0;
    let localConfig: TakeoverConfigSync = DEFAULT_TAKEOVER_CONFIG;

    const handleStorageChange = (
      changes: { [key: string]: chrome.storage.StorageChange },
      areaName: string,
    ) => {
      const cfgChange = changes[STORAGE_KEYS.TAKEOVER_CONFIG];
      if (areaName === 'local' && cfgChange?.newValue) {
        localConfig = cfgChange.newValue as TakeoverConfigSync;
      }
    };

    // 同步本地接管配置副本，供点击事件捕获阶段做零延迟同步快判
    try {
      if (typeof chrome !== 'undefined' && chrome.storage?.local) {
        chrome.storage.local.get([STORAGE_KEYS.TAKEOVER_CONFIG], (res) => {
          if (res?.[STORAGE_KEYS.TAKEOVER_CONFIG]) {
            localConfig = res[STORAGE_KEYS.TAKEOVER_CONFIG] as TakeoverConfigSync;
          }
        });
        chrome.storage.onChanged?.addListener(handleStorageChange);
      }
    } catch {
      // Ignore storage error
    }

    const mediaBar = new MediaBarManager();

    const cleanup = () => {
      try {
        abortController.abort();
      } catch {}

      try {
        mediaBar.stop();
      } catch {}

      try {
        domMediaObserver?.disconnect();
        clearTimeout(domMediaTimer);
      } catch {}

      try {
        if (typeof document !== 'undefined') {
          document.querySelectorAll('.sheepget-mediabar-host').forEach((el) => {
            el.remove();
          });
        }
      } catch {}
      try {
        if (typeof chrome !== 'undefined') {
          chrome.storage?.onChanged?.removeListener?.(handleStorageChange);
          chrome.runtime?.onMessage?.removeListener?.(handleRuntimeMessage);
        }
      } catch {}

      if (window.__SHEEPGET_CONTENT_CLEANUP__ === cleanup) {
        delete window.__SHEEPGET_CONTENT_CLEANUP__;
      }
    };

    window.__SHEEPGET_CONTENT_CLEANUP__ = cleanup;

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
        onContextInvalidated: cleanup,
      });
    }

    function handleClickEvent(e: MouseEvent) {
      let clickMask = localKeyMask;
      if (e.altKey) clickMask |= KEY_MASKS.Alt;
      if (e.ctrlKey) clickMask |= KEY_MASKS.Control;
      if (e.shiftKey) clickMask |= KEY_MASKS.Shift;

      let targetUrl: string | undefined;
      let el: Element | null = e.target instanceof Element ? e.target : null;
      while (el && el !== document.body) {
        if (el instanceof HTMLAnchorElement && el.href) {
          targetUrl = el.href;
          break;
        }
        el = el.parentElement;
      }

      if (clickMask > 0) {
        const msg: ShortcutClickMessage = {
          type: 'SHORTCUT_CLICKED',
          keyMask: clickMask,
          url: targetUrl,
          timestamp: Date.now(),
        };
        safeSendMessage(msg, undefined, {
          onContextInvalidated: cleanup,
        });
      }

      // 网页端捕获阶段超前判定与拦截：
      // 若点击的是有效的 HTTP/HTTPS 下载链接，且命中接管规则：
      // 在 DOM 捕获阶段立即阻止默认导航事件，彻底杜绝浏览器内部创建原生下载条目（从物理根源上消除飞入动画与气泡）。
      if (
        targetUrl &&
        (targetUrl.startsWith('http://') || targetUrl.startsWith('https://')) &&
        e.button === 0 // 仅对鼠标主键（左键）直接点击拦截，中键/右键留给浏览器原生菜单
      ) {
        const decision = decideTakeover(
          targetUrl,
          undefined,
          window.location.href,
          localConfig,
          clickMask,
        );

        if (decision.takeover) {
          e.preventDefault();
          e.stopPropagation();

          const handoverMsg: DirectClickHandoverMessage = {
            type: 'DIRECT_CLICK_HANDOVER',
            url: targetUrl,
            pageUrl: window.location.href,
            pageTitle: document.title,
            referrer: document.referrer,
          };
          safeSendMessage<HandoverResponse>(handoverMsg, (resp) => {
            // 若交接被桌面端拒绝且原因属于桌面端未连接（离线），回退将链接放行还给浏览器
            if (resp && !resp.accepted && resp.failure === 'not_delivered') {
              window.location.assign(targetUrl!);
            }
          });
        }
      }
    }

    function handleBlur() {
      localKeyMask = 0;
      const msg: ResetKeysMessage = { type: 'RESET_KEYS' };
      safeSendMessage(msg, undefined, {
        onContextInvalidated: cleanup,
      });
    }

    // Capture phase listeners (IDM style) to intercept keys and clicks before page handlers
    window.addEventListener('keydown', handleKeyEvent, { capture: true, signal });
    window.addEventListener('keyup', handleKeyEvent, { capture: true, signal });
    window.addEventListener('click', handleClickEvent, { capture: true, signal });
    window.addEventListener('auxclick', handleClickEvent, { capture: true, signal });
    window.addEventListener('blur', handleBlur, { signal });

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
      document.addEventListener('DOMContentLoaded', reportPageContext, { once: true, signal });
    } else {
      reportPageContext();
    }

    // Initialize media floating bar manager
    const startMediaBar = () => {
      mediaBar.start();
      reportDomMedia();
    };
    if (document.readyState === 'loading') {
      document.addEventListener('DOMContentLoaded', startMediaBar, { once: true, signal });
    } else {
      startMediaBar();
    }

    function reportDomMedia() {
      if (typeof document === 'undefined') return;
      const mediaElements = document.querySelectorAll('video, audio');
      const foundUrls = new Set<string>();

      for (const el of Array.from(mediaElements)) {
        const candidateUrls = extractMediaUrls(el as HTMLMediaElement);
        for (const u of candidateUrls) {
          if (u.startsWith('http://') || u.startsWith('https://')) {
            foundUrls.add(u);
          }
        }
      }

      if (foundUrls.size > 0) {
        const msg: ReportDomMediaMessage = {
          type: 'REPORT_DOM_MEDIA',
          resources: Array.from(foundUrls).map((url) => ({ url })),
        };
        safeSendMessage(msg);
      }
    }

    let domMediaTimer: number | undefined;
    let domMediaObserver: MutationObserver | undefined;
    try {
      domMediaObserver = new MutationObserver(() => {
        clearTimeout(domMediaTimer);
        domMediaTimer = window.setTimeout(() => {
          reportDomMedia();
        }, 300);
      });
      domMediaObserver.observe(document.body || document.documentElement, {
        childList: true,
        subtree: true,
      });
    } catch {}

    // Request currently detected media resources for this tab
    safeSendMessage<MediaResource[]>({ type: 'GET_TAB_MEDIA' }, (res) => {
      if (res && Array.isArray(res)) {
        mediaBar.setResources(res);
      }
    });

    const handleRuntimeMessage = (
      msg: BackgroundMessage,
      _sender: unknown,
      sendResponse: (report: KeyStateReport) => void,
    ) => {
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
    };

    // 之后新嗅探到的资源由 background 主动推送：播放器后加载、用户点了播放才发起
    // 媒体请求的页面，首次拉取必然为空，只能靠这条推送完成关联。
    try {
      chrome.runtime?.onMessage?.addListener(handleRuntimeMessage);
    } catch {
      // Extension context might be invalidated
    }
  },
});
