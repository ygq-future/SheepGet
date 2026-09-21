import { describe, it, afterEach } from 'node:test';
import assert from 'node:assert/strict';
import {
  computeBarPosition,
  MEDIA_BAR_MARGIN,
  MEDIA_BAR_VIEWPORT_PADDING,
  shouldShowDismissAll,
  MediaBarManager,
} from './mediabar';

interface Box {
  top: number;
  right: number;
  bottom: number;
  left: number;
  width: number;
  height: number;
}

const VIEWPORT = { width: 1024, height: 800 };
const BAR = { width: 150, height: 26 };

function box(over: Partial<Box> = {}): Box {
  return { top: 100, right: 800, bottom: 500, left: 200, width: 600, height: 400, ...over };
}

describe('mediabar positioning logic', () => {
  it('anchors the bar above the player, right-aligned to its right edge', () => {
    const pos = computeBarPosition(box(), VIEWPORT, BAR);

    assert.equal(pos.visible, true);
    // 右对齐：悬浮条右边缘与播放器右边缘对齐
    assert.equal(pos.left, 800 - BAR.width);
    // 在播放器上方，整条都在画面之外
    assert.equal(pos.top, 100 - BAR.height - MEDIA_BAR_MARGIN);
    assert.ok(pos.top + BAR.height <= 100, 'bar must not cover the player');
  });

  it('drops below the player when there is no room above, still not covering it', () => {
    const player = box({ top: 4, bottom: 404 });
    const pos = computeBarPosition(player, VIEWPORT, BAR);

    assert.equal(pos.visible, true);
    assert.equal(pos.left, 800 - BAR.width);
    assert.equal(pos.top, 404 + MEDIA_BAR_MARGIN);
    assert.ok(pos.top >= player.bottom, 'bar must not cover the player');
  });

  it('falls back inside the viewport when the player leaves no room either side', () => {
    // 占满整个视口的播放器：上下都没有外侧位置可放
    const pos = computeBarPosition(box({ top: -120, bottom: 880, height: 1000 }), VIEWPORT, BAR);

    assert.equal(pos.visible, true);
    assert.equal(pos.top, MEDIA_BAR_VIEWPORT_PADDING);
    assert.equal(pos.left, 800 - BAR.width);
  });

  it('hides the bar when the player is too small to host it', () => {
    assert.equal(computeBarPosition(box({ right: 280, width: 80 }), VIEWPORT, BAR).visible, false);
    assert.equal(
      computeBarPosition(box({ bottom: 140, height: 40 }), VIEWPORT, BAR).visible,
      false,
    );
  });

  it('hides the bar when the player scrolled out of the viewport', () => {
    assert.equal(
      computeBarPosition(box({ top: -500, bottom: -100 }), VIEWPORT, BAR).visible,
      false,
    );
    assert.equal(computeBarPosition(box({ top: 900, bottom: 1300 }), VIEWPORT, BAR).visible, false);
  });

  it('keeps the bar inside the viewport when the player overflows to the right', () => {
    const pos = computeBarPosition(box({ left: 0, right: 1200, width: 1200 }), VIEWPORT, BAR);

    assert.equal(pos.visible, true);
    assert.equal(pos.left, VIEWPORT.width - BAR.width - MEDIA_BAR_VIEWPORT_PADDING);
    assert.equal(pos.top, 100 - BAR.height - MEDIA_BAR_MARGIN);
  });

  it('clamps the top edge when the player is scrolled above the viewport', () => {
    const pos = computeBarPosition(box({ top: -50, bottom: 350 }), VIEWPORT, BAR);

    assert.equal(pos.visible, true);
    assert.equal(pos.top, 350 + MEDIA_BAR_MARGIN);
  });

  it('never leaves the viewport on the left when the player is scrolled away', () => {
    const pos = computeBarPosition(box({ left: -200, right: 100, width: 300 }), VIEWPORT, BAR);

    assert.equal(pos.visible, true);
    assert.equal(pos.left, MEDIA_BAR_VIEWPORT_PADDING);
    assert.equal(pos.top, 100 - BAR.height - MEDIA_BAR_MARGIN);
  });

  it('keeps a bar wider than the visible player on the left edge', () => {
    const pos = computeBarPosition(box({ left: 0, right: 200, width: 200 }), VIEWPORT, {
      width: 250,
      height: 26,
    });

    assert.equal(pos.visible, true);
    assert.equal(pos.left, MEDIA_BAR_VIEWPORT_PADDING);
  });
});

describe('mediabar dismiss-all logic', () => {
  it('only offers dismiss-all when multiple targets are active', () => {
    assert.equal(shouldShowDismissAll(0), false);
    assert.equal(shouldShowDismissAll(1), false);
    assert.equal(shouldShowDismissAll(2), true);
    assert.equal(shouldShowDismissAll(5), true);
  });

  it('tracks active player counts and clears all players on dismissAll', () => {
    const manager = new MediaBarManager();
    assert.equal(manager.isAllDismissed(), false);
    assert.equal(manager.getActivePlayerCount(), 0);

    // 模拟 3 个播放器对象
    const v1 = { isConnected: true } as HTMLVideoElement;
    const v2 = { isConnected: true } as HTMLVideoElement;
    const v3 = { isConnected: true } as HTMLVideoElement;

    // 手动注册到 manager 进行状态测试
    manager.registerTestPlayer(v1);
    assert.equal(manager.getActivePlayerCount(), 1);
    assert.equal(manager.shouldShowDismissAllOption(), false);

    manager.registerTestPlayer(v2);
    assert.equal(manager.getActivePlayerCount(), 2);
    assert.equal(manager.shouldShowDismissAllOption(), true);

    manager.registerTestPlayer(v3);
    assert.equal(manager.getActivePlayerCount(), 3);
    assert.equal(manager.shouldShowDismissAllOption(), true);

    // 单个关闭其中一个
    manager.dismissSinglePlayer(v1);
    assert.equal(manager.getActivePlayerCount(), 2);
    assert.equal(manager.shouldShowDismissAllOption(), true);

    // 单个再关闭一个，只剩 1 个
    manager.dismissSinglePlayer(v2);
    assert.equal(manager.getActivePlayerCount(), 1);
    assert.equal(manager.shouldShowDismissAllOption(), false);

    // 执行全部关闭
    manager.dismissAll();
    assert.equal(manager.isAllDismissed(), true);
    assert.equal(manager.getActivePlayerCount(), 0);
    assert.equal(manager.shouldShowDismissAllOption(), false);
  });
});

describe('mediabar resource visibility gating', () => {
  function createTestFixtures() {
    const container = {
      style: { display: 'none', left: '', top: '', right: '', bottom: '' },
    } as unknown as HTMLDivElement;

    const shadowRoot = {
      getElementById: () => ({
        getBoundingClientRect: () => ({ width: 100, height: 30 }),
        addEventListener: () => {},
        removeEventListener: () => {},
        innerHTML: '',
      }),
      querySelector: () => null,
      querySelectorAll: () => [],
    } as unknown as ShadowRoot;

    return { container, shadowRoot };
  }

  it('keeps bar hidden when player has no resource, and shows when resource associates', () => {
    const originalWindow = globalThis.window;
    const originalRaf = globalThis.requestAnimationFrame;
    const originalCaf = globalThis.cancelAnimationFrame;
    const originalChrome = (globalThis as unknown as { chrome?: unknown }).chrome;

    globalThis.window = {
      innerWidth: 1920,
      innerHeight: 1080,
    } as unknown as Window & typeof globalThis;
    globalThis.requestAnimationFrame = (cb: FrameRequestCallback) => {
      cb(0);
      return 1;
    };
    globalThis.cancelAnimationFrame = () => {};
    (globalThis as unknown as { chrome: unknown }).chrome = {
      runtime: { sendMessage: () => {} },
    };

    try {
      const manager = new MediaBarManager();
      const { container, shadowRoot } = createTestFixtures();

      const video = {
        isConnected: true,
        currentSrc: 'https://example.com/video.mp4',
        getBoundingClientRect: () => ({
          left: 50,
          top: 100,
          right: 450,
          bottom: 350,
          width: 400,
          height: 250,
        }),
      } as unknown as HTMLVideoElement;

      manager.registerTestPlayer(video, { container, shadowRoot });

      // 1. Initial state: no resources in tab, bar must remain display: none
      manager.setResources([]);
      assert.equal(container.style.display, 'none');

      // 2. Resource arrives and matches video URL
      manager.setResources([
        {
          id: 'res_1',
          url: 'https://example.com/video.mp4',
          tabId: 1,
          filename: 'video.mp4',
          mimeType: 'video/mp4',
          isHls: false,
          foundAt: Date.now(),
        },
      ]);
      assert.equal(container.style.display, 'block');

      // 3. Resources cleared on navigation -> bar hides again
      manager.setResources([]);
      assert.equal(container.style.display, 'none');
    } finally {
      globalThis.window = originalWindow;
      globalThis.requestAnimationFrame = originalRaf;
      globalThis.cancelAnimationFrame = originalCaf;
      (globalThis as unknown as { chrome: unknown }).chrome = originalChrome;
    }
  });

  it('associates dynamically attached HLS MSE blob streams when resources arrive', () => {
    const originalWindow = globalThis.window;
    const originalRaf = globalThis.requestAnimationFrame;
    const originalCaf = globalThis.cancelAnimationFrame;
    const originalChrome = (globalThis as unknown as { chrome?: unknown }).chrome;

    globalThis.window = {
      innerWidth: 1920,
      innerHeight: 1080,
    } as unknown as Window & typeof globalThis;
    globalThis.requestAnimationFrame = (cb: FrameRequestCallback) => {
      cb(0);
      return 1;
    };
    globalThis.cancelAnimationFrame = () => {};
    (globalThis as unknown as { chrome: unknown }).chrome = {
      runtime: { sendMessage: () => {} },
    };

    try {
      const manager = new MediaBarManager();
      const { container, shadowRoot } = createTestFixtures();

      // 1. Initially no resources sniffed yet while video tag is inserted
      const video = {
        isConnected: true,
        currentSrc: '',
        src: '',
        readyState: 0,
        paused: true,
        currentTime: 0,
        getBoundingClientRect: () => ({
          left: 50,
          top: 100,
          right: 450,
          bottom: 350,
          width: 400,
          height: 250,
        }),
      } as unknown as HTMLVideoElement;

      manager.registerTestPlayer(video, { container, shadowRoot });
      manager.setResources([]);
      assert.equal(container.style.display, 'none');

      // 2. Video initializes with MediaSource blob URL
      Object.defineProperty(video, 'currentSrc', {
        value: 'blob:https://example.com/mse-uuid-1234',
        writable: true,
      });

      // 3. Tab sniffs the m3u8 stream and pushes resources to mediabar
      manager.setResources([
        {
          id: 'res_hls',
          url: 'https://cdn.example.com/live/index.m3u8',
          tabId: 1,
          filename: 'stream.mp4',
          mimeType: 'application/vnd.apple.mpegurl',
          isHls: true,
          foundAt: Date.now(),
        },
      ]);

      // Now associated with the HLS resource and becomes visible!
      assert.equal(container.style.display, 'block');
    } finally {
      globalThis.window = originalWindow;
      globalThis.requestAnimationFrame = originalRaf;
      globalThis.cancelAnimationFrame = originalCaf;
      (globalThis as unknown as { chrome: unknown }).chrome = originalChrome;
    }
  });
});

describe('mediabar context invalidation and handover status flow', () => {
  const originalChrome = (globalThis as unknown as { chrome?: unknown }).chrome;

  afterEach(() => {
    (globalThis as unknown as { chrome?: unknown }).chrome = originalChrome;
  });

  function setupPlayer(manager: MediaBarManager) {
    let innerHTML = '';
    let title = '';
    const dlBtn = {
      set innerHTML(val: string) {
        innerHTML = val;
      },
      get innerHTML() {
        return innerHTML;
      },
      set title(val: string) {
        title = val;
      },
      get title() {
        return title;
      },
      addEventListener: () => {},
      removeEventListener: () => {},
    };

    const shadowRoot = {
      getElementById: (id: string) => (id === 'dl-btn' ? dlBtn : null),
      querySelector: () => null,
      querySelectorAll: () => [],
    } as unknown as ShadowRoot;

    const video = {
      isConnected: true,
      currentSrc: 'https://example.com/video.mp4',
    } as HTMLVideoElement;

    manager.registerTestPlayer(video, {
      shadowRoot,
      resource: {
        id: 'res_1',
        url: 'https://example.com/video.mp4',
        tabId: 1,
        filename: 'video.mp4',
        mimeType: 'video/mp4',
        isHls: false,
        foundAt: Date.now(),
      },
    });

    return { video, dlBtn };
  }

  it('shows reload prompt and does not throw when extension context is invalidated', () => {
    (globalThis as unknown as { chrome: unknown }).chrome = {
      runtime: {
        id: 'mock-id',
        sendMessage: () => {
          throw new Error('Extension context invalidated.');
        },
      },
    };

    const manager = new MediaBarManager();
    const { video, dlBtn } = setupPlayer(manager);

    manager.testTriggerDownload(video);

    assert.ok(dlBtn.innerHTML.includes('扩展已更新，点击刷新'));
    assert.ok(dlBtn.title.includes('扩展已重载或更新'));
    assert.equal(manager.isInvalidated(), true);
  });

  it('progresses from loading to success status when handover is accepted', () => {
    let handoverCallback: ((resp: unknown) => void) | null = null;
    (globalThis as unknown as { chrome: unknown }).chrome = {
      runtime: {
        id: 'mock-id',
        sendMessage: (_msg: unknown, cb: (resp: unknown) => void) => {
          handoverCallback = cb;
        },
      },
    };

    const manager = new MediaBarManager();
    const { video, dlBtn } = setupPlayer(manager);

    manager.testTriggerDownload(video);

    assert.ok(dlBtn.innerHTML.includes('正在投递…'));

    // Desktop accepts handover
    assert.ok(handoverCallback);
    (handoverCallback as (resp: unknown) => void)({ accepted: true });
    assert.ok(dlBtn.innerHTML.includes('已投递至桌面端 ✓'));
  });

  it('shows failure status when handover is rejected', () => {
    let handoverCallback: ((resp: unknown) => void) | null = null;
    (globalThis as unknown as { chrome: unknown }).chrome = {
      runtime: {
        id: 'mock-id',
        sendMessage: (_msg: unknown, cb: (resp: unknown) => void) => {
          handoverCallback = cb;
        },
      },
    };

    const manager = new MediaBarManager();
    const { video, dlBtn } = setupPlayer(manager);

    manager.testTriggerDownload(video);
    assert.ok(dlBtn.innerHTML.includes('正在投递…'));

    // Desktop rejects handover with reason
    assert.ok(handoverCallback);
    (handoverCallback as (resp: unknown) => void)({ accepted: false, reason: '桌面端未连接' });
    assert.ok(dlBtn.innerHTML.includes('移交失败 (桌面端未连接)'));
    assert.equal(dlBtn.title, '桌面端未连接');
  });
});
