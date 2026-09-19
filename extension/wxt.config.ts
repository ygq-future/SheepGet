import { defineConfig } from 'wxt';

// Fixed public key ensures deterministic 32-char extension ID: oediboaeofmnlkgcjhnpfnngphkjooam
// regardless of local file path or browser environment (ADR-0005 Decision 3).
const FIXED_PUBLIC_KEY =
  'MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAq5XDI+F/oNkLYJ4AgqQBbdeFTRJSrM3KqKCR25fMMHizIRl+mByiS+jipWapQG9EVupC3CXh1orOvuG9AmckIfFODKq8bOQrJCh0sNzOlL6291U7ntw+cRWz6225mc3HwG1qnW8sZX8GgqfM4Fh9x1LUneBEgJRserUlxHFRTojdkwifGxZ3GO/pt0Pkqjhyy5/U5v3i9Mwo0ZWfnroU5buyoLxvRaSJD1JucmedYMm3HInCfW7pHTlImBrKHvDUAF2VoWnUZVjPIa1n8+zEW5XavNN6AfAVfv7oVgfmYZEJRv52xGNtWzU5R+Ucgp8o/EntpPI2L2VwdkdK9vLXewIDAQAB';

export default defineConfig({
  extensionApi: 'chrome',
  modules: ['@wxt-dev/module-react'],
  outDir: '../dist-extension',
  manifest: {
    name: 'SheepGet Integration Module',
    description: 'Browser integration and media download helper for SheepGet',
    version: '1.0.0',
    key: FIXED_PUBLIC_KEY,
    // downloads.ui 用于 setUiOptions：接管可用时关掉 Chrome 自己的下载气泡与动画，
    // 避免「桌面端弹窗」和「文件飞向下载按钮」两套反馈同时出现。
    // alarms 用于链路保活探测：桌面端重启会换端口，服务 worker 被挂起后单靠启动时
    // 那一次初始化无法发现，需要周期性重算链路状态（见 entrypoints/background.ts）。
    permissions: [
      'downloads',
      'downloads.ui',
      'webRequest',
      'storage',
      'tabs',
      'scripting',
      'nativeMessaging',
      'alarms',
    ],
    host_permissions: ['<all_urls>'],
    action: {
      default_title: 'SheepGet Resources',
    },
  },
});
