export interface PageContext {
  pageTitle: string;
  pageUrl: string;
}

/**
 * 页面上下文的上报去重。
 *
 * 标题改写、前进/后退、hash 变化与页面 DOM 变化都会触发重读，但绝大多数与当前页面无关；
 * 只有 URL 或标题真的变了才发消息，避免把一次站内切换变成一串重复上报。
 */
export function createPageContextReporter(
  read: () => PageContext,
  send: (context: PageContext) => void,
): () => void {
  let reported = '';
  return () => {
    const context = read();
    const signature = `${context.pageUrl}\n${context.pageTitle}`;
    if (signature === reported) {
      return;
    }
    reported = signature;
    send(context);
  };
}
