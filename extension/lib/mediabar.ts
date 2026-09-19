import type { MediaResource } from './media';
import type { HandoverResponse } from './types';

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

// 悬浮条挂在播放器右上角的外侧：右边缘与播放器右边缘对齐，整体位于画面上方，
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

interface TrackedPlayer {
  video: HTMLVideoElement;
  container: HTMLDivElement;
  shadowRoot: ShadowRoot;
  dismissed: boolean;
  resource?: MediaResource;
}

export class MediaBarManager {
  private players = new Map<HTMLVideoElement, TrackedPlayer>();
  private intersectionObserver: IntersectionObserver;
  private resizeObserver: ResizeObserver;
  private mutationObserver: MutationObserver | null = null;
  private availableResources: MediaResource[] = [];
  private frame = 0;

  constructor() {
    // 播放器进出视口、被显示/隐藏都会由交叉观察器抛出，作为重算位置的触发点。
    this.intersectionObserver = new IntersectionObserver(
      () => {
        this.scheduleUpdate();
      },
      { threshold: [0, 0.1, 0.5, 1.0] },
    );

    // 播放器自身的尺寸会随站点布局变化（懒布局、剧场模式、响应式），
    // 不跟随重算就会停在首次测量到的位置上。
    this.resizeObserver = new ResizeObserver(() => {
      this.scheduleUpdate();
    });
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
    this.mutationObserver?.disconnect();
    this.intersectionObserver.disconnect();
    this.resizeObserver.disconnect();
    document.removeEventListener('scroll', this.handleViewportChange, { capture: true });
    window.removeEventListener('resize', this.handleViewportChange);
    document.removeEventListener('fullscreenchange', this.handleViewportChange);
    if (this.frame) {
      cancelAnimationFrame(this.frame);
      this.frame = 0;
    }

    for (const tracked of this.players.values()) {
      tracked.container.remove();
    }
    this.players.clear();
  }

  setResources(resources: MediaResource[]) {
    this.availableResources = resources;
    // Re-associate players with newly available resources
    for (const tracked of this.players.values()) {
      this.associateResource(tracked);
      this.renderBar(tracked);
    }
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
    const videos = document.querySelectorAll('video');
    for (const video of Array.from(videos)) {
      if (!this.players.has(video)) {
        this.attachPlayer(video);
      }
    }

    // Clean up removed videos
    for (const [video, tracked] of this.players.entries()) {
      if (!video.isConnected) {
        this.intersectionObserver.unobserve(video);
        this.resizeObserver.unobserve(video);
        tracked.container.remove();
        this.players.delete(video);
      }
    }
  }

  private attachPlayer(video: HTMLVideoElement) {
    // Create host container in page DOM
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

    this.intersectionObserver.observe(video);
    this.resizeObserver.observe(video);
  }

  private associateResource(tracked: TrackedPlayer) {
    const video = tracked.video;
    const currentSrc = video.currentSrc || video.src;

    // 1. Direct src match
    if (currentSrc && !currentSrc.startsWith('blob:')) {
      const match = this.availableResources.find((r) => r.url === currentSrc);
      if (match) {
        tracked.resource = match;
        return;
      }
    }

    // 2. If video has sources
    const sources = Array.from(video.querySelectorAll('source'));
    for (const s of sources) {
      if (s.src) {
        const match = this.availableResources.find((r) => r.url === s.src);
        if (match) {
          tracked.resource = match;
          return;
        }
      }
    }

    // 3. Fallback: if there is only 1 resource or first resource, associate it
    if (this.availableResources.length > 0 && !tracked.resource) {
      tracked.resource = this.availableResources[0];
    }
  }

  private update(tracked: TrackedPlayer) {
    if (tracked.dismissed) {
      tracked.container.style.display = 'none';
      return;
    }

    // 先让宿主可见，才量得到悬浮条的真实尺寸（宽度决定右对齐位置，高度决定落在播放器上方的 y）
    tracked.container.style.display = 'block';
    const bar = tracked.shadowRoot.getElementById('bar');
    const barRect = bar?.getBoundingClientRect();
    const position = computeBarPosition(
      tracked.video.getBoundingClientRect(),
      { width: window.innerWidth, height: window.innerHeight },
      { width: barRect?.width ?? 0, height: barRect?.height ?? 0 },
    );

    if (!position.visible) {
      tracked.container.style.display = 'none';
      return;
    }

    tracked.container.style.left = `${position.left}px`;
    tracked.container.style.top = `${position.top}px`;
    tracked.container.style.right = 'auto';
    tracked.container.style.bottom = 'auto';
  }

  private showStatus(tracked: TrackedPlayer, text: string, color: string, restoreMs = 2500) {
    const dlBtn = tracked.shadowRoot.getElementById('dl-btn');
    if (!dlBtn) return;
    dlBtn.innerHTML = `<span class="status-msg" style="color:${color}">${text}</span>`;
    setTimeout(() => {
      this.renderBar(tracked);
    }, restoreMs);
  }

  private renderBar(tracked: TrackedPlayer) {
    const shadow = tracked.shadowRoot;
    const resource = tracked.resource;
    const badgeText = resource?.isHls ? 'HLS' : '视频';

    shadow.innerHTML = `
      <style>
        .bar {
          display: inline-flex;
          align-items: center;
          background: rgba(15, 23, 42, 0.92);
          backdrop-filter: blur(8px);
          border: 1px solid rgba(255, 255, 255, 0.12);
          border-radius: 6px;
          padding: 2px 4px 2px 8px;
          gap: 6px;
          box-shadow: 0 4px 12px rgba(0, 0, 0, 0.35);
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
          color: #f8fafc;
          font-size: 12px;
          font-weight: 500;
          cursor: pointer;
          padding: 3px 6px;
          border-radius: 4px;
          transition: background 0.12s ease;
        }
        .btn-download:hover {
          background: rgba(255, 255, 255, 0.1);
        }
        .badge {
          background: ${resource?.isHls ? '#a855f7' : '#10b981'};
          color: #ffffff;
          font-size: 10px;
          font-weight: 600;
          padding: 1px 4px;
          border-radius: 3px;
          letter-spacing: 0.02em;
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
          font-size: 14px;
          cursor: pointer;
          border-radius: 3px;
          transition: color 0.12s, background 0.12s;
        }
        .btn-close:hover {
          color: #ffffff;
          background: rgba(255, 255, 255, 0.15);
        }
        .status-msg {
          font-size: 11px;
          color: #34d399;
          font-weight: 500;
          padding: 0 4px;
        }
      </style>
      <div class="bar" id="bar">
        <button class="btn-download" id="dl-btn">
          <span class="badge">${badgeText}</span>
          <span>下载视频</span>
        </button>
        <button class="btn-close" id="close-btn" title="关闭当前悬浮条 (刷新后恢复)">×</button>
      </div>
    `;

    const dlBtn = shadow.getElementById('dl-btn');
    const closeBtn = shadow.getElementById('close-btn');

    closeBtn?.addEventListener('click', (e) => {
      e.stopPropagation();
      tracked.dismissed = true;
      tracked.container.style.display = 'none';
    });

    dlBtn?.addEventListener('click', (e) => {
      e.stopPropagation();
      if (tracked.resource) {
        this.triggerDownload(tracked);
        return;
      }

      // 尚未关联到资源时即时向 background 补拉本标签页的嗅探结果
      this.showStatus(tracked, '正在探测资源…', '#94a3b8');
      chrome.runtime.sendMessage(
        { type: 'GET_TAB_MEDIA' },
        (resList: MediaResource[] | undefined) => {
          if (resList && resList.length > 0) {
            tracked.resource = resList[0];
            this.renderBar(tracked);
            this.triggerDownload(tracked);
            return;
          }
          this.showStatus(tracked, '未探测到可下载资源', '#f87171');
        },
      );
    });

    // 悬浮条自身宽度会随状态文案变化，渲染后立即重算锚点
    this.update(tracked);
  }

  private triggerDownload(tracked: TrackedPlayer) {
    const resource = tracked.resource;
    if (!resource) return;

    this.showStatus(tracked, '已投递至桌面端 ✓', '#34d399', 3000);

    chrome.runtime.sendMessage(
      { type: 'HANDOVER_MEDIA', resource },
      (resp: HandoverResponse | undefined) => {
        if (!resp?.accepted) {
          this.showStatus(tracked, '移交失败', '#f87171');
        }
      },
    );
  }
}
