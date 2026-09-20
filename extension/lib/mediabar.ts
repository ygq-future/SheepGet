import { formatBytes, type MediaResource } from './media';
import type { HandoverResponse, HLSVariantOption } from './types';

export const MEDIA_BAR_MARGIN = 10;
export const MEDIA_BAR_VIEWPORT_PADDING = 8;
export const MEDIA_BAR_MIN_VIDEO_WIDTH = 100;
export const MEDIA_BAR_MIN_VIDEO_HEIGHT = 60;

export interface RectLike {
  top: number;
  right: number;
  bottom: number;
  left: number;
  width: number;
  height: number;
}

export interface SizeLike {
  width: number;
  height: number;
}

export interface BarPosition {
  visible: boolean;
  left: number;
  top: number;
}

// 悬浮条停靠在对应播放器右上角外侧：右边缘与播放器对齐，整体位于画面上方，
// 因此不遮挡任何画面内容。上方放不下（播放器贴着视口顶、或被滚动到顶部）时退到播放器
// 下方，同样在画面之外；只有当播放器占满视口、上下都没有位置时才落回视口内。
// 播放器完全离开视口或小到放不下悬浮条时不显示。
export function computeBarPosition(
  videoRect: RectLike,
  viewport: SizeLike,
  bar: SizeLike,
): BarPosition {
  if (
    videoRect.width < MEDIA_BAR_MIN_VIDEO_WIDTH ||
    videoRect.height < MEDIA_BAR_MIN_VIDEO_HEIGHT
  ) {
    return { visible: false, left: 0, top: 0 };
  }

  const offscreen =
    videoRect.bottom <= 0 ||
    videoRect.right <= 0 ||
    videoRect.top >= viewport.height ||
    videoRect.left >= viewport.width;
  if (offscreen) {
    return { visible: false, left: 0, top: 0 };
  }

  const maxLeft = Math.max(
    MEDIA_BAR_VIEWPORT_PADDING,
    viewport.width - bar.width - MEDIA_BAR_VIEWPORT_PADDING,
  );
  const left = Math.min(Math.max(videoRect.right - bar.width, MEDIA_BAR_VIEWPORT_PADDING), maxLeft);

  const minTop = MEDIA_BAR_VIEWPORT_PADDING;
  const maxTop = Math.max(minTop, viewport.height - bar.height - MEDIA_BAR_VIEWPORT_PADDING);
  const above = videoRect.top - MEDIA_BAR_MARGIN - bar.height;
  const below = videoRect.bottom + MEDIA_BAR_MARGIN;
  const top = above >= minTop ? above : below <= maxTop ? below : minTop;

  return { visible: true, left, top };
}

export function shouldShowDismissAll(activeCount: number): boolean {
  return activeCount > 1;
}

interface TrackedPlayer {
  video: HTMLVideoElement;
  container: HTMLDivElement;
  shadowRoot: ShadowRoot;
  dismissed: boolean;
  resource?: MediaResource;
  /** 预拉取到的清晰度列表 */
  variants?: HLSVariantOption[];
  loadingVariants?: boolean;
  /** 悬浮条菜单里选定的清晰度地址；交接时随资源一起带上。 */
  variantUri?: string;
  statusTimer?: ReturnType<typeof setTimeout>;
  menuCloseTimer?: ReturnType<typeof setTimeout>;
  closeAllMenuTimer?: ReturnType<typeof setTimeout>;
  videoCleanup?: () => void;
}

export class MediaBarManager {
  private players = new Map<HTMLVideoElement, TrackedPlayer>();
  private intersectionObserver?: IntersectionObserver;
  private resizeObserver?: ResizeObserver;
  private mutationObserver: MutationObserver | null = null;
  private availableResources: MediaResource[] = [];
  private frame = 0;
  private allDismissed = false;

  constructor() {
    // 播放器进出视口、被显示/隐藏都会由交叉观察器抛出，作为重算位置的触发点。
    if (typeof IntersectionObserver !== 'undefined') {
      this.intersectionObserver = new IntersectionObserver(
        () => {
          this.scheduleUpdate();
        },
        { threshold: [0, 0.1, 0.5, 1.0] },
      );
    }

    // 播放器自身的尺寸会随站点布局变化（懒布局、剧场模式、响应式），
    // 不跟随重算就会停在首次测量到的位置上。
    if (typeof ResizeObserver !== 'undefined') {
      this.resizeObserver = new ResizeObserver(() => {
        this.scheduleUpdate();
      });
    }
  }

  start() {
    this.scanVideos();

    // Observe DOM mutations for dynamically inserted players
    this.mutationObserver = new MutationObserver(() => {
      this.scanVideos();
    });
    this.mutationObserver.observe(document.body || document.documentElement, {
      childList: true,
      subtree: true,
    });

    // 滚动事件不冒泡但会经过捕获路径，因此页面内滚动容器也能触发重算
    document.addEventListener('scroll', this.handleViewportChange, {
      capture: true,
      passive: true,
    });
    window.addEventListener('resize', this.handleViewportChange, { passive: true });
    document.addEventListener('fullscreenchange', this.handleViewportChange);
  }

  stop() {
    this.intersectionObserver?.disconnect();
    this.resizeObserver?.disconnect();
    this.mutationObserver?.disconnect();
    document.removeEventListener('scroll', this.handleViewportChange, { capture: true });
    window.removeEventListener('resize', this.handleViewportChange);
    document.removeEventListener('fullscreenchange', this.handleViewportChange);
    if (this.frame) {
      cancelAnimationFrame(this.frame);
      this.frame = 0;
    }

    for (const [video, tracked] of this.players.entries()) {
      this.teardownPlayer(video, tracked);
    }
    this.players.clear();
  }

  setResources(resources: MediaResource[]) {
    this.availableResources = resources;
    // Re-associate players with newly available resources
    for (const tracked of this.players.values()) {
      const prevResource = tracked.resource;
      this.associateResource(tracked);
      // 只有在关联资源真正发生改变（例如此前未关联，现在新探测到了资源；或者切换了视频源）时才重新渲染。
      // 绝不能在资源未变时反复调用 renderBar 重建 DOM，否则会造成闪烁并销毁正在展示的清晰度下拉菜单。
      if (tracked.resource !== prevResource) {
        if (!tracked.shadowRoot.querySelector('.variant-menu')) {
          this.renderBar(tracked);
        }
      }
    }
    this.scheduleUpdate();
  }

  isAllDismissed(): boolean {
    return this.allDismissed;
  }

  getActivePlayerCount(): number {
    if (this.allDismissed) return 0;
    let count = 0;
    for (const [video, tracked] of this.players.entries()) {
      if (!tracked.dismissed && (video.isConnected ?? true)) {
        count++;
      }
    }
    return count;
  }

  shouldShowDismissAllOption(): boolean {
    return !this.allDismissed && shouldShowDismissAll(this.getActivePlayerCount());
  }

  dismissSinglePlayer(video: HTMLVideoElement) {
    const tracked = this.players.get(video);
    if (tracked) {
      tracked.dismissed = true;
      if (tracked.container?.style) {
        tracked.container.style.display = 'none';
      }
      this.teardownPlayer(video, tracked);
    }
  }

  dismissAll() {
    this.allDismissed = true;
    for (const [video, tracked] of this.players.entries()) {
      tracked.dismissed = true;
      if (tracked.container?.style) {
        tracked.container.style.display = 'none';
      }
      this.teardownPlayer(video, tracked);
    }
  }

  private cleanupTrackedMenus(tracked: TrackedPlayer) {
    if (tracked.statusTimer) {
      clearTimeout(tracked.statusTimer);
      tracked.statusTimer = undefined;
    }
    if (tracked.menuCloseTimer) {
      clearTimeout(tracked.menuCloseTimer);
      tracked.menuCloseTimer = undefined;
    }
    if (tracked.closeAllMenuTimer) {
      clearTimeout(tracked.closeAllMenuTimer);
      tracked.closeAllMenuTimer = undefined;
    }
    tracked.shadowRoot?.querySelector?.('.variant-menu')?.remove();
    const closeAllMenu = tracked.shadowRoot?.querySelector?.('.close-all-menu');
    if (closeAllMenu) {
      closeAllMenu.remove();
      const bar = tracked.shadowRoot?.getElementById?.('bar');
      if (bar) {
        bar.style.borderBottomLeftRadius = '14px';
        bar.style.borderBottomRightRadius = '14px';
      }
    }
  }

  private teardownPlayer(video: HTMLVideoElement, tracked: TrackedPlayer) {
    this.intersectionObserver?.unobserve(video);
    this.resizeObserver?.unobserve(video);
    tracked.videoCleanup?.();
    this.cleanupTrackedMenus(tracked);
    tracked.container?.remove?.();
  }

  registerTestPlayer(video: HTMLVideoElement, tracked?: Partial<TrackedPlayer>) {
    this.players.set(video, {
      video,
      container: tracked?.container ?? ({} as HTMLDivElement),
      shadowRoot: tracked?.shadowRoot ?? ({} as ShadowRoot),
      dismissed: tracked?.dismissed ?? false,
      ...tracked,
    });
  }

  private handleViewportChange = () => {
    this.scheduleUpdate();
  };

  private scheduleUpdate() {
    if (this.frame) return;
    this.frame = requestAnimationFrame(() => {
      this.frame = 0;
      for (const tracked of this.players.values()) {
        this.update(tracked);
      }
    });
  }

  private scanVideos() {
    if (this.allDismissed) return;
    const videos = document.querySelectorAll('video');
    for (const video of Array.from(videos)) {
      if (!this.players.has(video)) {
        this.attachPlayer(video);
      }
    }

    // Clean up removed videos
    for (const [video, tracked] of this.players.entries()) {
      if (!video.isConnected) {
        this.teardownPlayer(video, tracked);
        this.players.delete(video);
      }
    }
  }

  private attachPlayer(video: HTMLVideoElement) {
    if (this.allDismissed) return;
    const container = document.createElement('div');
    container.className = 'sheepget-mediabar-host';
    container.style.position = 'fixed';
    container.style.zIndex = '2147483647'; // Max z-index
    container.style.pointerEvents = 'auto';
    container.style.display = 'none';

    // Shadow DOM to isolate styles from host page
    const shadowRoot = container.attachShadow({ mode: 'closed' });

    const tracked: TrackedPlayer = {
      video,
      container,
      shadowRoot,
      dismissed: false,
    };

    this.players.set(video, tracked);
    document.body.appendChild(container);

    this.associateResource(tracked);
    this.renderBar(tracked);

    this.intersectionObserver?.observe(video);

    const onVideoEvent = () => {
      const prevResource = tracked.resource;
      this.associateResource(tracked);
      if (tracked.resource !== prevResource) {
        this.renderBar(tracked);
      }
      this.scheduleUpdate();
    };

    video.addEventListener('loadedmetadata', onVideoEvent);
    video.addEventListener('playing', onVideoEvent);
    video.addEventListener('play', onVideoEvent);
    video.addEventListener('canplay', onVideoEvent);

    tracked.videoCleanup = () => {
      video.removeEventListener('loadedmetadata', onVideoEvent);
      video.removeEventListener('playing', onVideoEvent);
      video.removeEventListener('play', onVideoEvent);
      video.removeEventListener('canplay', onVideoEvent);
    };
    this.resizeObserver?.observe(video);
  }

  private associateResource(tracked: TrackedPlayer) {
    if (tracked.resource && this.availableResources.some((r) => r.url === tracked.resource?.url)) {
      return;
    }
    tracked.resource = undefined;
    const video = tracked.video;
    const currentSrc = video.currentSrc || video.src || '';

    // 1. Direct src match
    if (currentSrc && !currentSrc.startsWith('blob:')) {
      const match = this.availableResources.find((r) => r.url === currentSrc);
      if (match) {
        tracked.resource = match;
        if (match.isHls) this.prefetchVariants(tracked);
        return;
      }
    }

    // 2. Blob match (MSE / HLS player instance attached to blob: URL)
    if (currentSrc.startsWith('blob:')) {
      const hlsResource = this.availableResources.find((r) => r.isHls);
      if (hlsResource) {
        tracked.resource = hlsResource;
        this.prefetchVariants(tracked);
        return;
      }
      const single = this.availableResources[0];
      if (single) {
        tracked.resource = single;
        if (single.isHls) this.prefetchVariants(tracked);
        return;
      }
    }

    // 3. Fallback: single resource
    const single = this.availableResources[0];
    if (this.availableResources.length === 1 && single) {
      tracked.resource = single;
      if (single.isHls) this.prefetchVariants(tracked);
    }
  }

  private prefetchVariants(tracked: TrackedPlayer) {
    const resource = tracked.resource;
    if (!resource || !resource.isHls || tracked.variants || tracked.loadingVariants) return;
    tracked.loadingVariants = true;
    chrome.runtime.sendMessage(
      { type: 'GET_HLS_VARIANTS', url: resource.url, pageUrl: resource.pageUrl },
      (resp: { variants?: HLSVariantOption[] } | undefined) => {
        tracked.loadingVariants = false;
        tracked.variants = resp?.variants || [];
      },
    );
  }

  private update(tracked: TrackedPlayer) {
    if (this.allDismissed || tracked.dismissed) {
      if (tracked.container.style.display !== 'none') {
        tracked.container.style.display = 'none';
      }
      return;
    }

    if (!tracked.resource) {
      const prev = tracked.resource;
      this.associateResource(tracked);
      if (tracked.resource !== prev) {
        this.renderBar(tracked);
      }
    }

    if (!tracked.resource) {
      if (tracked.container.style.display !== 'none') {
        tracked.container.style.display = 'none';
      }
      return;
    }

    const bar = tracked.shadowRoot.getElementById('bar');
    const barRect = bar?.getBoundingClientRect();
    const position = computeBarPosition(
      tracked.video.getBoundingClientRect(),
      { width: window.innerWidth, height: window.innerHeight },
      { width: barRect?.width ?? 0, height: barRect?.height ?? 0 },
    );

    if (!position.visible) {
      if (tracked.container.style.display !== 'none') {
        tracked.container.style.display = 'none';
      }
      return;
    }

    if (tracked.container.style.display !== 'block') {
      tracked.container.style.display = 'block';
    }

    tracked.container.style.left = `${position.left}px`;
    tracked.container.style.top = `${position.top}px`;
    tracked.container.style.right = 'auto';
    tracked.container.style.bottom = 'auto';
  }
  private showStatus(tracked: TrackedPlayer, text: string, color: string, restoreMs = 2500) {
    const dlBtn = tracked.shadowRoot.getElementById('dl-btn');
    if (!dlBtn) return;
    if (tracked.statusTimer) {
      clearTimeout(tracked.statusTimer);
      tracked.statusTimer = undefined;
    }
    dlBtn.innerHTML = `<span class="status-msg" style="color:${color}">${text}</span>`;
    tracked.statusTimer = setTimeout(() => {
      if (!tracked.shadowRoot.querySelector('.variant-menu')) {
        this.renderBar(tracked);
      }
      tracked.statusTimer = undefined;
    }, restoreMs);
  }

  private renderBar(tracked: TrackedPlayer) {
    const shadow = tracked.shadowRoot;
    const resource = tracked.resource;
    const getFormatBadge = (res?: MediaResource): string => {
      if (!res) return 'VIDEO';
      if (res.isHls) return 'HLS';
      const ext = res.filename?.split('.').pop()?.toUpperCase();
      if (ext && ext.length <= 4 && /^[A-Z0-9]+$/.test(ext)) {
        return ext;
      }
      const mime = res.mimeType?.split('/').pop()?.toUpperCase();
      if (mime && mime.length <= 4 && /^[A-Z0-9]+$/.test(mime)) {
        return mime;
      }
      return 'MP4';
    };
    const badgeText = getFormatBadge(resource);
    shadow.innerHTML = `
      <style>
        .bar {
          position: relative;
          display: inline-flex;
          width: max-content;
          align-items: center;
          background: rgba(17, 20, 27, 0.94);
          backdrop-filter: blur(12px);
          border: 1px solid rgba(255, 255, 255, 0.1);
          border-radius: 14px;
          padding: 3px 4px 3px 5px;
          gap: 3px;
          box-shadow: inset 0 1px 0 0 rgba(255, 255, 255, 0.14), 0 8px 24px -4px rgba(0, 0, 0, 0.55);
          font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
          user-select: none;
          animation: sheepget-fade-in 0.15s ease-out;
        }
        @keyframes sheepget-fade-in {
          from { opacity: 0; transform: translateY(-4px); }
          to { opacity: 1; transform: translateY(0); }
        }
        .btn-download {
          display: inline-flex;
          align-items: center;
          gap: 5px;
          background: transparent;
          border: none;
          color: #f1f5f9;
          font-size: 11px;
          font-weight: 500;
          cursor: pointer;
          padding: 2px 5px;
          border-radius: 10px;
          transition: background 0.15s ease, color 0.15s ease;
        }
        .btn-download:hover {
          background: rgba(255, 255, 255, 0.08);
          color: #ffffff;
        }
        .dl-icon {
          color: #94a3b8;
          transition: color 0.15s ease, transform 0.15s ease;
        }
        .btn-download:hover .dl-icon {
          color: #38bdf8;
          transform: translateY(1px);
        }
        .badge {
          display: inline-flex;
          align-items: center;
          justify-content: center;
          height: 17px;
          box-sizing: border-box;
          background: ${resource?.isHls ? 'rgba(168, 85, 247, 0.14)' : 'rgba(16, 185, 129, 0.14)'};
          color: ${resource?.isHls ? '#c084fc' : '#34d399'};
          border: 1px solid ${resource?.isHls ? 'rgba(168, 85, 247, 0.28)' : 'rgba(16, 185, 129, 0.28)'};
          font-size: 9.5px;
          font-weight: 700;
          line-height: 1;
          padding: 0 5px;
          border-radius: 5px;
          letter-spacing: 0.03em;
          text-transform: uppercase;
        }
        .divider {
          width: 1px;
          height: 12px;
          background: rgba(255, 255, 255, 0.12);
          margin: 0 1px;
        }
        .btn-close {
          display: inline-flex;
          align-items: center;
          justify-content: center;
          width: 20px;
          height: 20px;
          background: transparent;
          border: none;
          color: #94a3b8;
          cursor: pointer;
          border-radius: 50%;
          transition: color 0.12s, background 0.12s;
        }
        .btn-close:hover {
          color: #ffffff;
          background: rgba(255, 255, 255, 0.14);
        }
        .status-msg {
          font-size: 11px;
          color: #34d399;
          font-weight: 500;
          padding: 0 4px;
          white-space: nowrap;
        }
      </style>
      <div class="bar" id="bar">
        <button class="btn-download" id="dl-btn" title="下载视频 (${badgeText})">
          <span class="badge">${badgeText}</span>
          <svg class="dl-icon" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.3" stroke-linecap="round" stroke-linejoin="round">
            <path d="M12 15V3m0 12l-4-4m4 4l4-4M2 17l.621 2.485A2 2 0 0 0 4.561 21h14.878a2 2 0 0 0 1.94-1.515L22 17" />
          </svg>
        </button>
        <div class="divider"></div>
        <button class="btn-close" id="close-btn" title="关闭当前悬浮条 (刷新后恢复)">
          <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.3" stroke-linecap="round" stroke-linejoin="round">
            <path d="M18 6L6 18M6 6l12 12" />
          </svg>
        </button>
      </div>
    `;

    const dlBtn = shadow.getElementById('dl-btn');
    const closeBtn = shadow.getElementById('close-btn');
    const barEl = shadow.getElementById('bar');

    closeBtn?.addEventListener('click', (e) => {
      e.stopPropagation();
      this.dismissSinglePlayer(tracked.video);
    });

    closeBtn?.addEventListener('mouseenter', () => {
      if (tracked.closeAllMenuTimer) {
        clearTimeout(tracked.closeAllMenuTimer);
        tracked.closeAllMenuTimer = undefined;
      }
      if (this.shouldShowDismissAllOption()) {
        this.showCloseAllMenu(tracked);
      }
    });

    closeBtn?.addEventListener('mouseleave', () => {
      this.scheduleHideCloseAllMenu(tracked);
    });

    const handleMouseEnter = () => {
      if (tracked.menuCloseTimer) {
        clearTimeout(tracked.menuCloseTimer);
        tracked.menuCloseTimer = undefined;
      }
      if (!tracked.resource) {
        return;
      }
      if (!tracked.resource.isHls) return;

      if (tracked.variants && tracked.variants.length > 1) {
        this.showVariantMenu(tracked, tracked.variants);
        return;
      }
      if (tracked.loadingVariants) {
        this.showLoadingMenu(tracked);
        return;
      }
      if (!tracked.variants) {
        this.showLoadingMenu(tracked);
        tracked.loadingVariants = true;
        chrome.runtime.sendMessage(
          {
            type: 'GET_HLS_VARIANTS',
            url: tracked.resource.url,
            pageUrl: tracked.resource.pageUrl,
          },
          (resp: { variants?: HLSVariantOption[] } | undefined) => {
            tracked.loadingVariants = false;
            tracked.variants = resp?.variants || [];
            if (tracked.variants.length > 1) {
              this.showVariantMenu(tracked, tracked.variants);
            } else {
              shadow.querySelector('.variant-menu')?.remove();
            }
          },
        );
      }
    };

    dlBtn?.addEventListener('mouseenter', handleMouseEnter);
    barEl?.addEventListener('mouseleave', () => {
      this.scheduleHideMenu(tracked);
    });

    dlBtn?.addEventListener('click', (e) => {
      e.stopPropagation();
      if (!tracked.resource) {
        chrome.runtime.sendMessage(
          { type: 'GET_TAB_MEDIA' },
          (resList: MediaResource[] | undefined) => {
            if (resList && resList.length > 0) {
              this.setResources(resList);
              if (tracked.resource) {
                this.beginDownload(tracked);
              }
            }
          },
        );
        return;
      }
      this.beginDownload(tracked);
    });

    // 悬浮条自身宽度会随状态文案变化，渲染后立即重算锚点
    this.update(tracked);
  }

  /**
   * 点击「下载视频」后的入口。HLS 资源在交接前先问桌面端有哪些清晰度：
   * 多于一个时弹出菜单让用户选定，选定（或只有一个/拿不到）后才交接。
   */
  private beginDownload(tracked: TrackedPlayer) {
    const resource = tracked.resource;
    if (!resource) return;

    if (!resource.isHls) {
      this.triggerDownload(tracked);
      return;
    }

    if (tracked.variants && tracked.variants.length > 1) {
      this.showVariantMenu(tracked, tracked.variants);
      return;
    }

    if (tracked.variants && tracked.variants.length <= 1) {
      this.triggerDownload(tracked);
      return;
    }

    this.showLoadingMenu(tracked);
    tracked.loadingVariants = true;
    chrome.runtime.sendMessage(
      { type: 'GET_HLS_VARIANTS', url: resource.url, pageUrl: resource.pageUrl },
      (resp: { variants?: HLSVariantOption[] } | undefined) => {
        tracked.loadingVariants = false;
        tracked.variants = resp?.variants || [];
        if (tracked.variants.length > 1) {
          this.showVariantMenu(tracked, tracked.variants);
          return;
        }
        tracked.shadowRoot.querySelector('.variant-menu')?.remove();
        this.triggerDownload(tracked);
      },
    );
  }

  private showLoadingMenu(tracked: TrackedPlayer) {
    const shadow = tracked.shadowRoot;
    if (shadow.querySelector('.variant-menu')) return;
    const menu = document.createElement('div');
    menu.className = 'variant-menu';
    menu.style.cssText =
      'position:absolute;top:100%;left:0;right:0;width:100%;box-sizing:border-box;' +
      'margin-top:4px;background:rgba(17,20,27,0.96);border:1px solid rgba(255,255,255,0.1);' +
      'border-radius:8px;box-shadow:inset 0 1px 0 0 rgba(255,255,255,0.12),0 12px 28px rgba(0,0,0,0.55);' +
      'padding:6px 10px;z-index:2;font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,sans-serif;' +
      'backdrop-filter:blur(12px);animation:sheepget-fade-in 0.12s ease-out;';

    const text = document.createElement('span');
    text.textContent = '正在读取清晰度…';
    text.style.cssText = 'font-size:11px;color:#94a3b8;white-space:nowrap;';
    menu.appendChild(text);

    menu.addEventListener('mouseenter', () => {
      if (tracked.menuCloseTimer) {
        clearTimeout(tracked.menuCloseTimer);
        tracked.menuCloseTimer = undefined;
      }
    });
    menu.addEventListener('mouseleave', () => {
      this.scheduleHideMenu(tracked);
    });

    const host = shadow.getElementById('bar');
    host?.appendChild(menu);
  }

  private scheduleHideMenu(tracked: TrackedPlayer) {
    if (tracked.menuCloseTimer) clearTimeout(tracked.menuCloseTimer);
    tracked.menuCloseTimer = setTimeout(() => {
      tracked.shadowRoot.querySelector('.variant-menu')?.remove();
      tracked.menuCloseTimer = undefined;
    }, 200);
  }

  private showVariantMenu(tracked: TrackedPlayer, variants: HLSVariantOption[]) {
    const shadow = tracked.shadowRoot;
    // 菜单追加在悬浮条之后，随它一起定位；选定或点空白后移除。
    shadow.querySelector('.variant-menu')?.remove();

    const video = tracked.video;
    const duration =
      (Number.isFinite(video.duration) && video.duration > 0 ? video.duration : 0) ||
      (tracked.resource?.probeDuration ?? 0);

    const sortedVariants = [...variants].sort((a, b) => (b.bandwidth || 0) - (a.bandwidth || 0));

    const menu = document.createElement('div');
    menu.className = 'variant-menu';
    menu.style.cssText =
      'position:absolute;top:100%;right:0;width:max-content;box-sizing:border-box;' +
      'margin-top:4px;background:rgba(17,20,27,0.96);border:1px solid rgba(255,255,255,0.1);' +
      'border-radius:8px;box-shadow:inset 0 1px 0 0 rgba(255,255,255,0.12),0 12px 28px rgba(0,0,0,0.55);' +
      'padding:2px;z-index:2;font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,sans-serif;' +
      'backdrop-filter:blur(12px);animation:sheepget-fade-in 0.12s ease-out;';

    for (const v of sortedVariants) {
      const item = document.createElement('button');
      item.type = 'button';
      item.style.cssText =
        'display:flex;width:100%;align-items:center;justify-content:space-between;gap:8px;' +
        'background:transparent;border:none;color:#f1f5f9;font-size:11px;padding:3px 6px;' +
        'border-radius:5px;cursor:pointer;text-align:left;transition:background 0.12s,color 0.12s;';
      item.addEventListener('mouseenter', () => {
        item.style.background = 'rgba(255,255,255,0.08)';
        item.style.color = '#ffffff';
      });
      item.addEventListener('mouseleave', () => {
        item.style.background = 'transparent';
        item.style.color = '#f1f5f9';
      });

      const label = document.createElement('span');
      label.textContent = v.label || '清晰度';
      label.style.cssText = 'font-weight:500;';
      item.appendChild(label);

      if (v.bandwidth && v.bandwidth > 0) {
        const bw = document.createElement('span');
        if (duration > 0) {
          const estBytes = (v.bandwidth * duration) / 8;
          bw.textContent = formatBytes(estBytes);
          bw.title = `预估大小: ${formatBytes(estBytes)} (码率: ${(v.bandwidth / 1e6).toFixed(1)} Mbps)`;
        } else {
          bw.textContent = `${(v.bandwidth / 1e6).toFixed(1)} Mbps`;
          bw.title = `码率: ${(v.bandwidth / 1e6).toFixed(1)} Mbps`;
        }
        bw.style.cssText =
          'font-size:10px;color:#94a3b8;flex-shrink:0;font-variant-numeric:tabular-nums;';
        item.appendChild(bw);
      }
      item.addEventListener('click', (e) => {
        e.stopPropagation();
        menu.remove();
        tracked.variantUri = v.uri;
        this.triggerDownload(tracked);
      });
      menu.appendChild(item);
    }

    menu.addEventListener('mouseenter', () => {
      if (tracked.menuCloseTimer) {
        clearTimeout(tracked.menuCloseTimer);
        tracked.menuCloseTimer = undefined;
      }
    });
    menu.addEventListener('mouseleave', () => {
      this.scheduleHideMenu(tracked);
    });

    const host = shadow.getElementById('bar');
    host?.appendChild(menu);

    // 点击菜单与悬浮条外部时关闭。使用 pointerdown + composedPath 穿透 Shadow DOM
    const close = (ev: Event) => {
      const path = ev.composedPath();
      if (!path.includes(menu) && !path.includes(tracked.container)) {
        menu.remove();
        document.removeEventListener('pointerdown', close, true);
      }
    };
    setTimeout(() => {
      document.addEventListener('pointerdown', close, true);
    }, 0);
  }

  private triggerDownload(tracked: TrackedPlayer) {
    const resource = tracked.resource;
    if (!resource) return;

    this.showStatus(tracked, '已投递至桌面端 ✓', '#34d399', 3000);

    chrome.runtime.sendMessage(
      { type: 'HANDOVER_MEDIA', resource: { ...resource, variantUri: tracked.variantUri } },
      (resp: HandoverResponse | undefined) => {
        if (!resp?.accepted) {
          this.showStatus(tracked, '移交失败', '#f87171');
        }
      },
    );
  }

  private showCloseAllMenu(tracked: TrackedPlayer) {
    const shadow = tracked.shadowRoot;
    if (shadow.querySelector('.close-all-menu')) return;
    const bar = shadow.getElementById('bar');
    if (!bar) return;

    bar.style.borderBottomLeftRadius = '0';
    bar.style.borderBottomRightRadius = '0';

    const btn = document.createElement('button');
    btn.type = 'button';
    btn.className = 'close-all-menu';
    btn.style.cssText =
      'position:absolute;top:100%;left:-1px;right:-1px;width:calc(100% + 2px);margin-top:-1px;box-sizing:border-box;' +
      'display:flex;align-items:center;justify-content:center;gap:5px;' +
      'background:rgba(17,20,27,0.96);border:1px solid rgba(255,255,255,0.1);border-top:1px solid rgba(255,255,255,0.06);' +
      'border-radius:0 0 14px 14px;box-shadow:0 8px 20px rgba(0,0,0,0.45);padding:4px 8px;z-index:2;' +
      'font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,sans-serif;' +
      'font-size:11px;font-weight:500;line-height:16px;color:#94a3b8;letter-spacing:-0.01em;cursor:pointer;' +
      'white-space:nowrap;backdrop-filter:blur(12px);transition:all 0.15s ease;animation:sheepget-fade-in 0.12s ease-out;';
    btn.innerHTML = `
      <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.3" stroke-linecap="round" stroke-linejoin="round">
        <circle cx="12" cy="12" r="10" />
        <path d="M4.93 4.93l14.14 14.14" />
      </svg>
      <span>全部关闭</span>
    `;
    btn.title = '关闭当前页面所有悬浮条 (刷新后恢复)';
    btn.addEventListener('mouseenter', () => {
      if (tracked.closeAllMenuTimer) {
        clearTimeout(tracked.closeAllMenuTimer);
        tracked.closeAllMenuTimer = undefined;
      }
      btn.style.background = 'rgba(239,68,68,0.16)';
      btn.style.color = '#fca5a5';
      btn.style.borderColor = 'rgba(239,68,68,0.3)';
    });
    btn.addEventListener('mouseleave', () => {
      btn.style.background = 'rgba(17,20,27,0.96)';
      btn.style.color = '#94a3b8';
      btn.style.borderColor = 'rgba(255,255,255,0.1)';
      this.scheduleHideCloseAllMenu(tracked);
    });

    btn.addEventListener('click', (e) => {
      e.stopPropagation();
      this.dismissAll();
    });

    bar.appendChild(btn);
  }

  private scheduleHideCloseAllMenu(tracked: TrackedPlayer) {
    if (tracked.closeAllMenuTimer) clearTimeout(tracked.closeAllMenuTimer);
    tracked.closeAllMenuTimer = setTimeout(() => {
      const shadow = tracked.shadowRoot;
      const menu = shadow?.querySelector?.('.close-all-menu');
      if (menu) {
        menu.remove();
        const bar = shadow?.getElementById?.('bar');
        if (bar) {
          bar.style.borderBottomLeftRadius = '14px';
          bar.style.borderBottomRightRadius = '14px';
        }
      }
      tracked.closeAllMenuTimer = undefined;
    }, 200);
  }
}
