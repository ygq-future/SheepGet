// @vitest-environment happy-dom
import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, render, screen } from '@testing-library/react';
import { FileInfoView } from './FileInfoView';
import { useFileInfoDraftStore } from '../stores/fileInfoDraft';
import { emptyFileInfoDraft } from '../lib/fileInfoDraft';
import * as windowModels from '../../bindings/sheep-get/internal/window/models';

const bindings = vi.hoisted(() => ({
  getActiveFileInfo: vi.fn(),
}));

vi.mock('../../bindings/sheep-get/app', () => ({
  GetActiveFileInfo: bindings.getActiveFileInfo,
  SubmitFileInfo: vi.fn(),
  CancelCurrentFileInfo: vi.fn(),
  MinimiseFileInfoWindow: vi.fn(),
  SetFileInfoWindowHeight: vi.fn(),
  SelectDirectory: vi.fn(),
  ProbeURL: vi.fn(),
  ProbeMediaDuration: vi.fn(() => Promise.resolve(0)),
  CheckFileConflict: vi.fn(() => Promise.resolve({ exists: false, suggestedFilename: '' })),
  CheckURLFilesExist: vi.fn(() => Promise.resolve({})),
  ShowProgressWindow: vi.fn(),
  SwitchFileInfoActive: vi.fn(),
  ResolveDestination: vi.fn(() => Promise.resolve({ categoryId: 'builtin-file', directory: '' })),
  ResolveCategoryDestination: vi.fn(() =>
    Promise.resolve({ categoryId: 'builtin-file', directory: '' }),
  ),
  ResolveDuplicateDecision: vi.fn(),
  AssignExtensionToCategory: vi.fn(),
  SetCategoryDirectory: vi.fn(),
  SelectHLSVariant: vi.fn(),
  GetSettings: vi.fn(() => Promise.resolve(null)),
  UpdateSettings: vi.fn(),
  GetStorageInfo: vi.fn(() => Promise.resolve(null)),
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

class ResizeObserverStub {
  observe() {}
  unobserve() {}
  disconnect() {}
}

function queuedItem(): windowModels.FileInfoItem {
  return new windowModels.FileInfoItem({
    id: 'queue-1',
    url: 'https://example.com/file.zip',
    filename: 'file.zip',
    directory: 'C:/Downloads',
    categoryDirectory: 'C:/Downloads',
    maxConn: 8,
    totalBytes: 4096,
    mimeType: 'application/zip',
    resumable: true,
    fileConflict: false,
    preDownload: false,
    queueIndex: 1,
    queueTotal: 1,
    probing: false,
  });
}

describe('FileInfoView', () => {
  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
    bindings.getActiveFileInfo.mockReset();
    useFileInfoDraftStore.setState({
      draft: emptyFileInfoDraft(),
      drafts: {},
      activeItemId: null,
      probing: false,
      loading: false,
    });
  });

  it('renders the active queue item into the editable form', async () => {
    vi.stubGlobal('ResizeObserver', ResizeObserverStub);
    bindings.getActiveFileInfo.mockResolvedValue(queuedItem());

    render(<FileInfoView />);

    expect(await screen.findByDisplayValue('https://example.com/file.zip')).toBeTruthy();
    expect(screen.getByDisplayValue('file.zip')).toBeTruthy();
    expect(screen.getByText('新建下载')).toBeTruthy();
  });
});
