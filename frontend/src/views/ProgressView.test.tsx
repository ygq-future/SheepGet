// @vitest-environment happy-dom
import { afterEach, describe, expect, it, vi } from 'vitest';
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { ProgressView } from './ProgressView';
import * as taskModels from '../../bindings/sheep-get/internal/task/models';
import { Event } from '../lib/protocol.generated';

const bindings = vi.hoisted(() => ({
  listTasks: vi.fn(),
  hideProgressWindow: vi.fn(),
  sealBundleTask: vi.fn(),
}));

// 进度窗口的行为由事件驱动：把注册进来的处理函数留下，测试才能像桌面端那样推一条任务更新。
const eventHandlers = vi.hoisted(() => new Map<string, (event: unknown) => void>());

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
  SealBundleTask: bindings.sealBundleTask,
}));

// 运行时的真实入口带副作用（会尝试连接开发服务器），而生成模型只需要 Create 的编解码辅助。
vi.mock('@wailsio/runtime', () => {
  const passthrough = (value: unknown) => value;
  return {
    Events: {
      On: vi.fn((name: string, handler: (event: unknown) => void) => {
        eventHandlers.set(name, handler);
        return () => eventHandlers.delete(name);
      }),
    },
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
    eventHandlers.clear();
    bindings.listTasks.mockReset();
    bindings.hideProgressWindow.mockReset();
    bindings.sealBundleTask.mockReset();
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

  it('renders a collecting zip batch and seals it on demand', async () => {
    vi.stubGlobal('ResizeObserver', ResizeObserverStub);
    const collecting = new taskModels.Task({
      id: 'task-collecting',
      filename: 'model-pack.zip',
      directory: 'C:/Downloads',
      status: taskModels.Status.StatusCollecting,
      totalBytes: 2048,
      downloaded: 0,
      bundle: new taskModels.ZipBundle({
        zipId: 'pack-1',
        name: 'model-pack.zip',
        filesTotal: 3,
        filesDone: 0,
        knownBytes: 2048,
        sealed: false,
      }),
      createdAt: '2026-10-29T00:00:00.000Z',
      updatedAt: '2026-10-29T00:00:00.000Z',
    });
    bindings.listTasks.mockResolvedValue([collecting]);

    render(<ProgressView />);

    expect(await screen.findByText('model-pack.zip')).toBeTruthy();
    expect(screen.getByText('收集中')).toBeTruthy();
    expect(screen.getByText('已收集 3 个文件')).toBeTruthy();
    expect(screen.getByText('等待确认')).toBeTruthy();

    fireEvent.click(screen.getByTitle('立即打包并开始下载'));
    await vi.waitFor(() => expect(bindings.sealBundleTask).toHaveBeenCalledWith('task-collecting'));
  });

  it('drops the completed card when the task itself disabled keep completed info', async () => {
    vi.stubGlobal('ResizeObserver', ResizeObserverStub);
    const task = downloadingTask('quiet.mp4');
    bindings.listTasks.mockResolvedValue([task]);

    render(<ProgressView />);
    expect(await screen.findByText('quiet.mp4')).toBeTruthy();

    act(() => {
      eventHandlers.get(Event.TaskUpdated)?.({
        data: {
          ...task,
          status: taskModels.Status.StatusCompleted,
          // 全局默认保留完成信息；这次下载按次关掉了它，所以完成后不留下这一条并收窗。
          progressDisplay: new taskModels.ProgressDisplay({ keepCompletedInfo: false }),
        },
      });
    });

    await vi.waitFor(() => expect(bindings.hideProgressWindow).toHaveBeenCalled());
    expect(screen.queryByText('quiet.mp4')).toBeNull();
  });
});
