/**
 * Chrome 在 onCreated 给出的文件名是从 URL 推出来的，真实文件名往往只存在于响应头里。
 * GitHub Release 资产就是这种形态：路径末段是一个 GUID，真正的名字
 * （PowerShell-7.6.6-win-x64.msi）只在 Content-Disposition 里。
 *
 * 接管规则按后缀决定，用 URL 推出来的名字判定会直接漏接；交接给桌面端时也需要真名，
 * 否则桌面端会先按没有后缀的名字命中「文件」分类。所以把 onHeadersReceived 拿到的
 * 响应头文件名按 URL 记下来，供 onCreated 查用。
 *
 * 下载在响应头到达后立刻创建，条目只活几秒；这里只按条数上限淘汰，避免无上限增长。
 */
export class ResponseFilenameCache {
  private readonly entries = new Map<string, string>();

  constructor(private readonly limit = 50) {}

  remember(url: string, filename: string): void {
    if (!url || !filename) return;
    // 同一个 URL 重新记录时先删除，保证淘汰顺序按最近一次写入。
    this.entries.delete(url);
    this.entries.set(url, filename);
    while (this.entries.size > this.limit) {
      const oldest = this.entries.keys().next();
      if (oldest.done) break;
      this.entries.delete(oldest.value);
    }
  }

  /** 依次按给定的 URL 查询；通常先给 finalUrl，再给原始 url。 */
  lookup(...urls: (string | undefined)[]): string | undefined {
    for (const url of urls) {
      if (!url) continue;
      const hit = this.entries.get(url);
      if (hit) return hit;
    }
    return undefined;
  }
}
