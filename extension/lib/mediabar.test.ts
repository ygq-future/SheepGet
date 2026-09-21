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

describe('mediabar dynamic size updates & overflow protection', () => {
  it('re-aligns position when status text expands width to prevent right overflow', () => {
    const originalWindow = globalThis.window;
    const originalRaf = globalThis.requestAnimationFrame;
    const originalCaf = globalThis.cancelAnimationFrame;

    globalThis.window = {
      innerWidth: 1000,
      innerHeight: 800,
    } as unknown as Window & typeof globalThis;
    globalThis.requestAnimationFrame = (cb: FrameRequestCallback) => {
      cb(0);
      return 1;
    };
    globalThis.cancelAnimationFrame = () => {};

    try {
      const manager = new MediaBarManager();
      let barWidth = 70;
      const container = {
        style: { display: 'none', left: '0px', top: '0px', right: '', bottom: '' },
      } as unknown as HTMLDivElement;

      const dlBtn = {
        innerHTML: '',
        title: '',
        addEventListener: () => {},
        removeEventListener: () => {},
      };

      const bar = {
        getBoundingClientRect: () => ({ width: barWidth, height: 26 }),
        addEventListener: () => {},
        removeEventListener: () => {},
      };

      const shadowRoot = {
        getElementById: (id: string) => {
          if (id === 'bar') return bar;
          if (id === 'dl-btn') return dlBtn;
          return null;
        },
        querySelector: () => null,
        querySelectorAll: () => [],
      } as unknown as ShadowRoot;

      // 视频宽度 600，right 为 990（距离视口右边界仅 10px）
      const video = {
        isConnected: true,
        currentSrc: 'https://example.com/video.mp4',
        getBoundingClientRect: () => ({
          left: 390,
          top: 100,
          right: 990,
          bottom: 450,
          width: 600,
          height: 350,
        }),
      } as unknown as HTMLVideoElement;

      manager.registerTestPlayer(video, { container, shadowRoot });
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

      // 初始状态下 barWidth = 70，left 应该为 990 - 70 = 920px
      assert.equal(container.style.left, '920px');
      // 右边界为 920 + 70 = 990px <= 1000 - 8px (992px)

      // 模拟点击下载触发 showStatus：文案变成长文本“已投递至桌面端 ✓”，barWidth 膨胀到 150px
      barWidth = 150;
      (
        manager as unknown as { showStatus: (t: unknown, text: string, color: string) => void }
      ).showStatus(manager.getTrackedPlayer(video), '已投递至桌面端 ✓', '#10b981');

      // 验证：left 必须被立即重新计算为适应 150px 宽度的位置！
      // 990 - 150 = 840px。右边缘 840 + 150 = 990px，绝不能留在 920px（否则 920 + 150 = 1070 溢出视口 1000）
      const leftNum = parseInt(container.style.left, 10);
      assert.ok(
        leftNum + barWidth <= 1000 - MEDIA_BAR_VIEWPORT_PADDING,
        `Bar right edge (${leftNum + barWidth}) must not exceed viewport right edge (992)`,
      );
      assert.equal(container.style.left, '840px');
    } finally {
      globalThis.window = originalWindow;
      globalThis.requestAnimationFrame = originalRaf;
      globalThis.cancelAnimationFrame = originalCaf;
    }
  });
});

describe('mediabar iframe tracking and cross-frame delegation', () => {
  it('positions floating bar outside and above an iframe player on top frame', () => {
    const originalWindow = globalThis.window;
    const originalRaf = globalThis.requestAnimationFrame;
    const originalCaf = globalThis.cancelAnimationFrame;

    globalThis.window = {
      innerWidth: 1200,
      innerHeight: 900,
    } as unknown as Window & typeof globalThis;
    globalThis.requestAnimationFrame = (cb: FrameRequestCallback) => {
      cb(0);
      return 1;
    };
    globalThis.cancelAnimationFrame = () => {};

    try {
      const manager = new MediaBarManager();
      const container = {
        style: { display: 'none', left: '', top: '', right: '', bottom: '' },
      } as unknown as HTMLDivElement;

      const shadowRoot = {
        getElementById: (id: string) =>
          id === 'bar'
            ? {
                getBoundingClientRect: () => ({ width: 100, height: 28 }),
                addEventListener: () => {},
                removeEventListener: () => {},
              }
            : null,
        querySelector: () => null,
        querySelectorAll: () => [],
      } as unknown as ShadowRoot;

      // 模拟顶层页面中的 iframe
      const iframe = {
        isConnected: true,
        tagName: 'IFRAME',
        getBoundingClientRect: () => ({
          left: 100,
          top: 150,
          right: 900,
          bottom: 600,
          width: 800,
          height: 450,
        }),
      } as unknown as HTMLIFrameElement;

      manager.registerTestPlayer(iframe, { container, shadowRoot });
      manager.setResources([
        {
          id: 'res_1',
          url: 'https://example.com/video.m3u8',
          tabId: 1,
          filename: 'video.m3u8',
          mimeType: 'application/vnd.apple.mpegurl',
          isHls: true,
          foundAt: Date.now(),
        },
      ]);

      // 悬浮条应该显示，且位于 iframe 上方外侧
      assert.equal(container.style.display, 'block');
      assert.equal(container.style.left, `${900 - 100}px`);
      assert.equal(container.style.top, `${150 - 28 - MEDIA_BAR_MARGIN}px`);
    } finally {
      globalThis.window = originalWindow;
      globalThis.requestAnimationFrame = originalRaf;
      globalThis.cancelAnimationFrame = originalCaf;
    }
  });

  it('suppresses local bar in child frame when claimed by parent, but shows in fullscreen', () => {
    const originalWindow = globalThis.window;
    const originalDocument = globalThis.document;
    const originalRaf = globalThis.requestAnimationFrame;
    const originalCaf = globalThis.cancelAnimationFrame;

    let fullscreenEl: Element | null = null;
    globalThis.window = {
      innerWidth: 800,
      innerHeight: 450,
    } as unknown as Window & typeof globalThis;
    globalThis.document = {
      get fullscreenElement() {
        return fullscreenEl;
      },
      addEventListener: () => {},
      removeEventListener: () => {},
    } as unknown as Document;
    globalThis.requestAnimationFrame = (cb: FrameRequestCallback) => {
      cb(0);
      return 1;
    };
    globalThis.cancelAnimationFrame = () => {};

    try {
      const manager = new MediaBarManager();
      const container = {
        style: { display: 'none', left: '', top: '', right: '', bottom: '' },
      } as unknown as HTMLDivElement;

      const shadowRoot = {
        getElementById: (id: string) =>
          id === 'bar'
            ? {
                getBoundingClientRect: () => ({ width: 100, height: 28 }),
                addEventListener: () => {},
                removeEventListener: () => {},
              }
            : null,
        querySelector: () => null,
        querySelectorAll: () => [],
      } as unknown as ShadowRoot;

      const video = {
        isConnected: true,
        currentSrc: 'https://example.com/video.mp4',
        getBoundingClientRect: () => ({
          left: 0,
          top: 0,
          right: 800,
          bottom: 450,
          width: 800,
          height: 450,
        }),
      } as unknown as HTMLVideoElement;

      manager.registerTestPlayer(video, { container, shadowRoot });
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

      // 未委托前：正常显示（在子 iframe 内）
      assert.equal(container.style.display, 'block');

      // 顶层框架认领了该视频框架
      manager.setPlayerDelegated(video, true);
      assert.equal(container.style.display, 'none');

      // 进入全屏：子 frame 内部的悬浮条解除 suppression 重新显示
      fullscreenEl = video as unknown as Element;
      manager.handleFullscreenChange();
      assert.equal(container.style.display, 'block');

      // 退出全屏：重新隐藏
      fullscreenEl = null;
      manager.handleFullscreenChange();
      assert.equal(container.style.display, 'none');
    } finally {
      globalThis.window = originalWindow;
      globalThis.document = originalDocument;
      globalThis.requestAnimationFrame = originalRaf;
      globalThis.cancelAnimationFrame = originalCaf;
    }
  });

  it('handles SHEEPGET_PLAYER_FRAME_DETECTED in top frame, replies claim and attaches iframe', () => {
    const originalDocument = globalThis.document;
    const originalWindow = globalThis.window;
    const originalRaf = globalThis.requestAnimationFrame;
    const originalCaf = globalThis.cancelAnimationFrame;

    globalThis.window = {
      innerWidth: 1000,
      innerHeight: 800,
    } as unknown as Window & typeof globalThis;
    globalThis.requestAnimationFrame = (cb: FrameRequestCallback) => {
      cb(0);
      return 1;
    };
    globalThis.cancelAnimationFrame = () => {};

    try {
      const manager = new MediaBarManager();
      const mockChildWindow = {} as Window;
      let postedBackMessage: unknown = null;
      mockChildWindow.postMessage = (msg: unknown) => {
        postedBackMessage = msg;
      };

      const iframe = {
        isConnected: true,
        tagName: 'IFRAME',
        contentWindow: mockChildWindow,
        getBoundingClientRect: () => ({
          left: 100,
          top: 200,
          right: 900,
          bottom: 700,
          width: 800,
          height: 500,
        }),
      } as unknown as HTMLIFrameElement;

      const hostDiv = {
        className: '',
        style: { position: '', zIndex: '', pointerEvents: '', display: '' },
        attachShadow: () => ({
          getElementById: () => null,
          querySelector: () => null,
          querySelectorAll: () => [],
          innerHTML: '',
        }),
      };

      globalThis.document = {
        querySelectorAll: (sel: string) => (sel === 'iframe' ? [iframe] : []),
        createElement: () => hostDiv,
        body: { appendChild: () => {} },
      } as unknown as Document;

      // 模拟接收到子框架发送的 DETECTED 消息
      const event = {
        source: mockChildWindow,
        data: {
          type: 'SHEEPGET_PLAYER_FRAME_DETECTED',
          version: 1,
          src: 'https://example.com/stream.m3u8',
        },
      } as MessageEvent;

      manager.handleWindowMessage(event);

      // 验证：给子窗口回复了 CLAIMED 确认
      assert.deepEqual(postedBackMessage, {
        type: 'SHEEPGET_PLAYER_FRAME_CLAIMED',
        version: 1,
      });

      // 验证：顶层 manager 成功追踪了该 iframe
      const tracked = manager.getTrackedPlayer(iframe);
      assert.ok(tracked, 'iframe player should be tracked in manager');
      assert.equal(tracked?.initialSrc, 'https://example.com/stream.m3u8');
    } finally {
      globalThis.document = originalDocument;
      globalThis.window = originalWindow;
      globalThis.requestAnimationFrame = originalRaf;
      globalThis.cancelAnimationFrame = originalCaf;
    }
  });
});
