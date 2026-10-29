// @vitest-environment happy-dom
import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, render, screen } from '@testing-library/react';
import { ProgressView } from './ProgressView';
import * as taskModels from '../../bindings/sheep-get/internal/task/models';

const bindings = vi.hoisted(() => ({
  listTasks: vi.fn(),
  hideProgressWindow: vi.fn(),
}));

vi.mock('../../bindings/sheep-get/app', () => ({
  ListTasks: bindings.listTasks,
  GetSettings: vi.fn(() => Promise.resolve(null)),
  UpdateSettings: vi.fn(),
  GetStorageInfo: vi.fn(() => Promise.resolve(null)),
  IsProgressWindowAlwaysOnTop: vi.fn(() => Promise.resolve(false)),
  ToggleProgressWindowAlwaysOnTop: vi.fn(() => Promise.resolve(false)),
  SetProgressWindowHeight: vi.fn(),
  HideProgressWindow: bindings.hideProgressWindow,
  MinimiseProgressWindow: vi.fn(),
  OpenFile: vi.fn(),
  OpenFolder: vi.fn(),
  PauseTask: vi.fn(),
  ResumeTask: vi.fn(),
  RetryTask: vi.fn(),
  RetryProcessingTask: vi.fn(),
}));

// 运行时的真实入口带副作用（会尝试连接开发服务器），而生成模型只需要 Create 的编解码辅助。
vi.mock('@wailsio/runtime', () => {
  const passthrough = (value: unknown) => value;
  return {
    Events: { On: vi.fn(() => () => {}) },
    Create: Object.assign(passthrough, {
      Any: passthrough,
      Array: () => passthrough,
      Map: () => passthrough,
      Nullable: passthrough,
      Events: {},
    }),
  };
});

// happy-dom 的 WAAPI 实现在动画被取消时留下未处理的 AbortError；去掉 animate 让 motion
// 退回 requestAnimationFrame 路径。动画本身不是这里的验证目标。
Reflect.deleteProperty(Element.prototype, 'animate');

// happy-dom 不实现布局观察；窗口高度跟随不是这里要验证的行为，给一个空实现即可。
class ResizeObserverStub {
  observe() {}
  unobserve() {}
  disconnect() {}
}

function downloadingTask(filename: string): taskModels.Task {
  return new taskModels.Task({
    id: `task-${filename}`,
    url: `https://example.com/${filename}`,
    filename,
    directory: 'C:/Downloads',
    totalBytes: 1024,
    downloaded: 512,
    speed: 0,
    status: taskModels.Status.StatusDownloading,
    maxConcurrency: 8,
    resumable: true,
    createdAt: '2026-10-29T00:00:00.000Z',
    updatedAt: '2026-10-29T00:00:00.000Z',
  });
}

describe('ProgressView', () => {
  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
    bindings.listTasks.mockReset();
    bindings.hideProgressWindow.mockReset();
  });

  it('renders an active download from the task list', async () => {
    vi.stubGlobal('ResizeObserver', ResizeObserverStub);
    bindings.listTasks.mockResolvedValue([downloadingTask('movie.mp4')]);

    render(<ProgressView />);

    expect(await screen.findByText('movie.mp4')).toBeTruthy();
    expect(screen.getByText(/下载进度/)).toBeTruthy();
  });

  it('closes the window when there is nothing active to show', async () => {
    vi.stubGlobal('ResizeObserver', ResizeObserverStub);
    bindings.listTasks.mockResolvedValue([]);

    render(<ProgressView />);

    await vi.waitFor(() => expect(bindings.hideProgressWindow).toHaveBeenCalled());
    expect(screen.queryByText('movie.mp4')).toBeNull();
  });
});
