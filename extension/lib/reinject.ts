/**
 * 遍历所有已有标签页并重新注入扩展的内容脚本与样式表。
 * 在扩展被安装、更新或开发者重载时由 Background Service Worker 触发，
 * 使得用户无需手动刷新已打开的网页即可使最新脚本生效。
 */
export async function injectContentScriptsIntoExistingTabs(
  chromeApi?: typeof chrome,
): Promise<{ successTabs: number; failedTabs: number }> {
  const api =
    chromeApi ?? (typeof chrome !== 'undefined' ? chrome : (undefined as unknown as typeof chrome));
  if (!api || !api.scripting || !api.tabs || !api.runtime?.getManifest) {
    return { successTabs: 0, failedTabs: 0 };
  }

  let manifest: chrome.runtime.Manifest;
  try {
    manifest = api.runtime.getManifest();
  } catch {
    return { successTabs: 0, failedTabs: 0 };
  }

  const contentScripts = manifest.content_scripts || [];
  if (!contentScripts.length) {
    return { successTabs: 0, failedTabs: 0 };
  }

  let tabs: chrome.tabs.Tab[] = [];
  try {
    const allTabs = await api.tabs.query({});
    tabs = allTabs.filter(
      (tab) =>
        tab.id !== undefined &&
        tab.id !== null &&
        typeof tab.url === 'string' &&
        (tab.url.startsWith('http://') || tab.url.startsWith('https://')),
    );
  } catch {
    return { successTabs: 0, failedTabs: 0 };
  }

  let successTabs = 0;
  let failedTabs = 0;

  await Promise.allSettled(
    tabs.map(async (tab) => {
      const tabId = tab.id;
      if (tabId === undefined || tabId === null) return;

      try {
        for (const cs of contentScripts) {
          if (cs.css && cs.css.length > 0) {
            try {
              await api.scripting.insertCSS({
                target: { tabId, allFrames: cs.all_frames ?? true },
                files: cs.css,
              });
            } catch {
              // 某些页面包含严格 CSP/跨域受限的 iframe，allFrames 注入会抛错；
              // 此时降级只向顶层主框架注入，确保主页面样式与功能不被子 frame 拖垮。
              await api.scripting
                .insertCSS({
                  target: { tabId },
                  files: cs.css,
                })
                .catch(() => {});
            }
          }
          if (cs.js && cs.js.length > 0) {
            try {
              await api.scripting.executeScript({
                target: { tabId, allFrames: cs.all_frames ?? true },
                files: cs.js,
              });
            } catch {
              // 同样降级只向顶层主框架注入
              await api.scripting
                .executeScript({
                  target: { tabId },
                  files: cs.js,
                })
                .catch(() => {});
            }
          }
        }
        console.log('[SheepGet] Re-injected content script into tab', tabId);
        successTabs++;
      } catch {
        failedTabs++;
      }
    }),
  );

  return { successTabs, failedTabs };
}
