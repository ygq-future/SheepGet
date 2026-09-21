/**
 * 检查当前扩展上下文是否有效。
 * 当扩展在后台被重载、更新或禁用时，残留在页面的孤儿 Content Script
 * 其 chrome.runtime 上下文会失效，调用相关 API 会抛出 "Extension context invalidated"。
 */
export function isExtensionContextValid(): boolean {
  try {
    return Boolean(
      typeof chrome !== 'undefined' &&
      chrome?.runtime &&
      typeof chrome.runtime.id === 'string' &&
      chrome.runtime.id.length > 0,
    );
  } catch {
    return false;
  }
}

export interface SafeSendMessageOptions {
  onContextInvalidated?: () => void;
}

/**
 * 安全地向扩展后台发送消息，捕获上下文失效错误，杜绝未捕获的 Extension context invalidated 报错。
 * @returns boolean 表示是否成功发起调用（若已失效返回 false）
 */
export function safeSendMessage<T = unknown>(
  message: unknown,
  responseCallback?: (response: T | undefined) => void,
  options?: SafeSendMessageOptions,
): boolean {
  if (!isExtensionContextValid()) {
    options?.onContextInvalidated?.();
    return false;
  }

  try {
    chrome.runtime.sendMessage(message, (res: T | undefined) => {
      const err = chrome.runtime.lastError;
      if (err) {
        const errMsg = String(err.message || '');
        if (
          errMsg.includes('Extension context invalidated') ||
          errMsg.includes('context invalidated') ||
          !isExtensionContextValid()
        ) {
          options?.onContextInvalidated?.();
          return;
        }
      }
      responseCallback?.(res);
    });
    return true;
  } catch (err: unknown) {
    const errMsg = String((err as Error)?.message || err);
    if (errMsg.includes('Extension context invalidated') || !isExtensionContextValid()) {
      options?.onContextInvalidated?.();
    }
    return false;
  }
}
