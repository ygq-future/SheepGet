import { useState, type SyntheticEvent } from 'react';
import * as Dialog from '@radix-ui/react-dialog';
import { Plus, X, Folder, Link as LinkIcon, Cpu } from 'lucide-react';
import { Select, type SelectOption } from './ui/Select';

interface AddTaskModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  defaultDir: string;
  onAdd: (url: string, dir: string, filename: string, maxConn: number) => Promise<void>;
}

const CONCURRENCY_OPTIONS: SelectOption<number>[] = [
  { value: 1, label: '1 通道', description: '单流保守下载' },
  { value: 2, label: '2 通道', description: '双路平衡分块' },
  { value: 4, label: '4 通道 (推荐)', description: '推荐主力性能' },
  { value: 8, label: '8 通道', description: '高速宽带并发' },
  { value: 16, label: '16 通道', description: '极限多流冲刺' },
];

export function AddTaskModal({ open, onOpenChange, defaultDir, onAdd }: AddTaskModalProps) {
  const [url, setUrl] = useState('');
  const [dir, setDir] = useState('');
  const [filename, setFilename] = useState('');
  const [maxConn, setMaxConn] = useState(4);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');

  const currentDir = dir || defaultDir;

  const handleSubmit = (e: SyntheticEvent) => {
    e.preventDefault();
    if (!url.trim()) {
      setError('请输入有效的下载链接');
      return;
    }
    setError('');
    setLoading(true);
    void (async () => {
      try {
        await onAdd(url.trim(), currentDir.trim(), filename.trim(), maxConn);
        setUrl('');
        setFilename('');
        onOpenChange(false);
      } catch (err) {
        setError(err instanceof Error ? err.message : '添加任务失败');
      } finally {
        setLoading(false);
      }
    })();
  };

  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog.Portal>
        <Dialog.Overlay className="animate-in fade-in fixed inset-0 z-50 bg-black/75 backdrop-blur-md transition-opacity" />
        <Dialog.Content className="animate-in fade-in zoom-in-95 fixed top-1/2 left-1/2 z-50 w-full max-w-lg -translate-x-1/2 -translate-y-1/2 rounded-2xl border border-white/10 bg-zinc-950/95 p-6 shadow-2xl shadow-black/80 backdrop-blur-2xl transition-all duration-200 focus:outline-hidden">
          {/* Header */}
          <div className="flex items-center justify-between border-b border-white/[0.06] pb-4">
            <div className="flex items-center gap-2.5">
              <div className="flex h-7 w-7 items-center justify-center rounded-lg border border-emerald-500/20 bg-emerald-500/10 text-emerald-400">
                <Plus className="h-4 w-4" />
              </div>
              <div>
                <Dialog.Title className="text-sm font-semibold tracking-tight text-zinc-100">
                  新建下载任务
                </Dialog.Title>
                <p className="text-[11px] text-zinc-400">
                  支持 HTTP / HTTPS 普通文件与多流分块传输
                </p>
              </div>
            </div>
            <Dialog.Close className="rounded-lg p-1.5 text-zinc-400 transition-colors hover:bg-white/5 hover:text-zinc-200">
              <X className="h-4 w-4" />
            </Dialog.Close>
          </div>

          <form onSubmit={handleSubmit} className="mt-5 space-y-4">
            {error && (
              <div className="animate-in fade-in flex items-center gap-2 rounded-xl border border-rose-500/20 bg-rose-500/10 p-3 text-xs text-rose-300">
                <span className="h-1.5 w-1.5 shrink-0 rounded-full bg-rose-400" />
                <span>{error}</span>
              </div>
            )}

            <div className="space-y-1.5">
              <label className="flex items-center gap-1.5 text-[11px] font-medium tracking-wide text-zinc-400 uppercase">
                <LinkIcon className="h-3.5 w-3.5 text-zinc-500" />
                资源链接 (URL)
              </label>
              <input
                type="text"
                value={url}
                onChange={(e) => setUrl(e.target.value)}
                placeholder="https://example.com/file.zip"
                className="w-full rounded-xl border border-white/10 bg-zinc-900/80 px-3.5 py-2.5 text-xs text-zinc-100 placeholder-zinc-500 shadow-inner transition-all duration-150 hover:border-white/20 focus:border-emerald-500/50 focus:ring-2 focus:ring-emerald-500/20 focus:outline-hidden"
                autoFocus
              />
            </div>

            <div className="grid grid-cols-2 gap-3.5">
              <div className="space-y-1.5">
                <label className="text-[11px] font-medium tracking-wide text-zinc-400 uppercase">
                  重命名 (可选)
                </label>
                <input
                  type="text"
                  value={filename}
                  onChange={(e) => setFilename(e.target.value)}
                  placeholder="留空自动解析"
                  className="w-full rounded-xl border border-white/10 bg-zinc-900/80 px-3.5 py-2 text-xs text-zinc-100 placeholder-zinc-500 shadow-inner transition-all duration-150 hover:border-white/20 focus:border-emerald-500/50 focus:ring-2 focus:ring-emerald-500/20 focus:outline-hidden"
                />
              </div>

              <div className="space-y-1.5">
                <label className="flex items-center gap-1 text-[11px] font-medium tracking-wide text-zinc-400 uppercase">
                  <Cpu className="h-3.5 w-3.5 text-zinc-500" />
                  并发通道数
                </label>
                <Select
                  value={maxConn}
                  onChange={setMaxConn}
                  options={CONCURRENCY_OPTIONS}
                  className="rounded-xl py-2"
                />
              </div>
            </div>

            <div className="space-y-1.5">
              <label className="flex items-center gap-1.5 text-[11px] font-medium tracking-wide text-zinc-400 uppercase">
                <Folder className="h-3.5 w-3.5 text-zinc-500" />
                保存路径
              </label>
              <input
                type="text"
                value={currentDir}
                onChange={(e) => setDir(e.target.value)}
                className="w-full rounded-xl border border-white/10 bg-zinc-900/80 px-3.5 py-2 text-xs text-zinc-100 placeholder-zinc-500 shadow-inner transition-all duration-150 hover:border-white/20 focus:border-emerald-500/50 focus:ring-2 focus:ring-emerald-500/20 focus:outline-hidden"
              />
            </div>

            <div className="flex items-center justify-end gap-2.5 border-t border-white/[0.06] pt-3">
              <button
                type="button"
                onClick={() => onOpenChange(false)}
                className="rounded-xl border border-white/10 px-4 py-2 text-xs font-medium text-zinc-300 transition-colors hover:bg-white/5 hover:text-zinc-100"
              >
                取消
              </button>
              <button
                type="submit"
                disabled={loading}
                className="rounded-xl bg-emerald-500 px-5 py-2 text-xs font-semibold text-zinc-950 shadow-lg shadow-emerald-500/20 transition-all duration-150 hover:bg-emerald-400 disabled:opacity-50"
              >
                {loading ? '正在分析...' : '立即下载'}
              </button>
            </div>
          </form>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
