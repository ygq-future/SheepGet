import { defineConfig } from 'wxt';

// Fixed public key ensures deterministic 32-char extension ID: oediboaeofmnlkgcjhnpfnngphkjooam
// regardless of local file path or browser environment (ADR-0005 Decision 3).
const FIXED_PUBLIC_KEY =
  'MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAq5XDI+F/oNkLYJ4AgqQBbdeFTRJSrM3KqKCR25fMMHizIRl+mByiS+jipWapQG9EVupC3CXh1orOvuG9AmckIfFODKq8bOQrJCh0sNzOlL6291U7ntw+cRWz6225mc3HwG1qnW8sZX8GgqfM4Fh9x1LUneBEgJRserUlxHFRTojdkwifGxZ3GO/pt0Pkqjhyy5/U5v3i9Mwo0ZWfnroU5buyoLxvRaSJD1JucmedYMm3HInCfW7pHTlImBrKHvDUAF2VoWnUZVjPIa1n8+zEW5XavNN6AfAVfv7oVgfmYZEJRv52xGNtWzU5R+Ucgp8o/EntpPI2L2VwdkdK9vLXewIDAQAB';

export default defineConfig({
  extensionApi: 'chrome',
  modules: ['@wxt-dev/module-react'],
  outDir: '../build/dist-extension',
  manifest: {
    name: 'SheepGet Integration Module',
    description: 'Browser integration and media download helper for SheepGet',
    key: FIXED_PUBLIC_KEY,
    // 接管按次进行（网页点击层超前拦截 + onCreated 极速 pause），不依赖 downloads.ui：
    // downloads.ui 的 setUiOptions 作用于整个 profile，会压制未接管与快捷键放行的下载反馈。
    // alarms 用于链路保活探测：桌面端重启会换端口，服务 worker 被挂起后单靠启动时
    // 那一次初始化无法发现，需要周期性重算链路状态（见 entrypoints/background.ts）。
    permissions: [
      'downloads',
      'webRequest',
      'webNavigation',
      'storage',
      'tabs',
      'scripting',
      'alarms',
    ],
    host_permissions: ['<all_urls>'],
    icons: {
      16: '/icons/16.png',
      32: '/icons/32.png',
      48: '/icons/48.png',
      128: '/icons/128.png',
    },
    action: {
      default_title: 'SheepGet Resources',
      default_icon: {
        16: '/icons/16.png',
        24: '/icons/24.png',
        32: '/icons/32.png',
      },
    },
  },
});
