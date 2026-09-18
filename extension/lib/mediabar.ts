import type { MediaResource } from './media';
import type { HandoverResponse } from './types';

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
  private mutationObserver: MutationObserver | null = null;
  private availableResources: MediaResource[] = [];

  constructor() {
    this.intersectionObserver = new IntersectionObserver(
      (entries) => {
        for (const entry of entries) {
          const video = entry.target as HTMLVideoElement;
          const tracked = this.players.get(video);
          if (!tracked) continue;

          if (!entry.isIntersecting || entry.intersectionRatio < 0.1) {
            tracked.container.style.display = 'none';
          } else if (!tracked.dismissed) {
            tracked.container.style.display = 'block';
            this.positionBar(tracked);
          }
        }
      },
      { threshold: [0, 0.1, 0.5, 1.0] },
    );
  }

  start() {
    // 1. Initial scan
    this.scanVideos();

    // 2. Observe DOM mutations for dynamically inserted players
    this.mutationObserver = new MutationObserver(() => {
      this.scanVideos();
    });
    this.mutationObserver.observe(document.body || document.documentElement, {
      childList: true,
      subtree: true,
    });

    // 3. Scroll and resize listeners to maintain top-right alignment
    window.addEventListener('scroll', this.handleViewportChange, { passive: true });
    window.addEventListener('resize', this.handleViewportChange, { passive: true });

    // 4. Listen for fullscreen changes
    document.addEventListener('fullscreenchange', this.handleViewportChange);
  }

  stop() {
    this.mutationObserver?.disconnect();
    this.intersectionObserver.disconnect();
    window.removeEventListener('scroll', this.handleViewportChange);
    window.removeEventListener('resize', this.handleViewportChange);
    document.removeEventListener('fullscreenchange', this.handleViewportChange);

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
    for (const tracked of this.players.values()) {
      if (!tracked.dismissed && tracked.container.style.display !== 'none') {
        this.positionBar(tracked);
      }
    }
  };

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

    // Initial position
    this.positionBar(tracked);
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

  private positionBar(tracked: TrackedPlayer) {
    const rect = tracked.video.getBoundingClientRect();

    // If video has no visible dimensions, hide
    if (rect.width < 100 || rect.height < 60) {
      tracked.container.style.display = 'none';
      return;
    }

    // Position 10px from top and 10px from right of the video element
    const top = Math.max(8, rect.top + 10);
    const right = Math.max(8, window.innerWidth - rect.right + 10);

    tracked.container.style.top = `${top}px`;
    tracked.container.style.right = `${right}px`;
    tracked.container.style.left = 'auto';
    tracked.container.style.bottom = 'auto';
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
      <div class="bar">
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
      const currentRes = tracked.resource;
      if (!currentRes) {
        // If no resource is associated yet, request from background
        chrome.runtime.sendMessage({ type: 'GET_TAB_MEDIA' }, (resList: MediaResource[]) => {
          if (resList && resList.length > 0) {
            tracked.resource = resList[0];
            this.triggerDownload(tracked);
          }
        });
        return;
      }
      this.triggerDownload(tracked);
    });
  }

  private triggerDownload(tracked: TrackedPlayer) {
    if (!tracked.resource) return;
    const shadow = tracked.shadowRoot;
    const dlBtn = shadow.getElementById('dl-btn');
    if (dlBtn) {
      dlBtn.innerHTML = `<span class="status-msg">已投递至桌面端 ✓</span>`;
      setTimeout(() => {
        this.renderBar(tracked);
      }, 3000);
    }

    // Message background to handover
    chrome.runtime.sendMessage(
      { type: 'HANDOVER_MEDIA', resource: tracked.resource },
      (resp: HandoverResponse | undefined) => {
        if (!resp?.accepted && dlBtn) {
          dlBtn.innerHTML = `<span style="color:#f87171;font-size:11px;">移交失败</span>`;
          setTimeout(() => {
            this.renderBar(tracked);
          }, 2500);
        }
      },
    );
  }
}
