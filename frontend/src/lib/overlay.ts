/**
 * 管理全局浮层（下拉框、弹出菜单等）对非客户区（Non-client Region）标题栏拖拽状态的锁定。
 * 当存在活跃浮层时，临时移除标题栏的 caption 属性，使其降级为普通网页客户区，从而能正常捕获外部点击（Outside Click）完成收起。
 */
let activeOverlayCount = 0;

export function acquireOverlayLock(): () => void {
  activeOverlayCount++;
  if (activeOverlayCount === 1 && typeof document !== 'undefined') {
    document.documentElement.setAttribute('data-has-overlay', 'true');
  }
  let released = false;
  return () => {
    if (released) return;
    released = true;
    activeOverlayCount = Math.max(0, activeOverlayCount - 1);
    if (activeOverlayCount === 0 && typeof document !== 'undefined') {
      document.documentElement.removeAttribute('data-has-overlay');
    }
  };
}

/** 仅供测试环境重置计数器 */
export function resetOverlayLockForTesting(): void {
  activeOverlayCount = 0;
  if (typeof document !== 'undefined') {
    document.documentElement.removeAttribute('data-has-overlay');
  }
}
