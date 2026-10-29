// @vitest-environment happy-dom
import { afterEach, describe, expect, it, vi } from 'vitest';
import { initSettingsListener } from './settings';

const runtime = vi.hoisted(() => ({ on: vi.fn() }));

vi.mock('../../bindings/sheep-get/app', () => ({
  GetSettings: vi.fn(() => Promise.resolve(null)),
  UpdateSettings: vi.fn(),
  GetStorageInfo: vi.fn(() => Promise.resolve(null)),
}));

// 运行时的真实入口带副作用（会尝试连接开发服务器），这里只关心监听注册本身。
vi.mock('@wailsio/runtime', () => {
  const passthrough = (value: unknown) => value;
  return {
    Events: { On: runtime.on },
    Create: Object.assign(passthrough, {
      Any: passthrough,
      Array: () => passthrough,
      Map: () => passthrough,
      Nullable: passthrough,
      Events: {},
    }),
  };
});

describe('initSettingsListener', () => {
  afterEach(() => {
    vi.restoreAllMocks();
    runtime.on.mockReset();
  });

  it('removes every listener it registered when disposed', () => {
    const addEventListener = vi.fn();
    const removeEventListener = vi.fn();
    vi.spyOn(window, 'matchMedia').mockReturnValue({
      addEventListener,
      removeEventListener,
    } as unknown as MediaQueryList);

    const cancels: Array<ReturnType<typeof vi.fn>> = [];
    runtime.on.mockImplementation(() => {
      const cancel = vi.fn();
      cancels.push(cancel);
      return cancel;
    });

    const dispose = initSettingsListener();

    // 系统配色与两处事件监听：设置更新、主题系统变更。
    expect(runtime.on).toHaveBeenCalledTimes(2);
    expect(addEventListener).toHaveBeenCalledTimes(1);
    const [, themeHandler] = addEventListener.mock.calls[0] as [string, () => void];

    dispose();

    expect(removeEventListener).toHaveBeenCalledWith('change', themeHandler);
    expect(cancels).toHaveLength(2);
    for (const cancel of cancels) expect(cancel).toHaveBeenCalledTimes(1);
  });
});
