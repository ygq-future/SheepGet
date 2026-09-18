# SheepGet 浏览器配套扩展安装与侧载指引

本指引介绍如何在 Google Chrome 与 Microsoft Edge 中手动加载 SheepGet 配套扩展（Manifest V3），并配置 Native Messaging Host 静默唤起跳板。

---

## 1. 扩展构建产物

项目已通过 WXT 框架完成工程化封装，构建命令：

```bash
# 在项目根目录执行完整门禁（自动构建扩展）：
bun run quality

# 或仅构建扩展产物：
bun run --cwd extension build
```

构建后输出目录为根目录下的 `dist-extension/chrome-mv3/`。

---

## 2. 固定扩展标识（Extension ID）

为保证 Native Messaging 宿主通信白名单（`allowed_origins`）在所有目录与环境下 100% 恒定，扩展已绑定固定的 2048 位公钥，在 Chrome 和 Edge 中生成的扩展 ID 全局固定为：

```text
oediboaeofmnlkgcjhnpfnngphkjooam
```

---

## 3. Chrome / Edge 侧载加载步骤

### Chrome 浏览器：

1. 打开 Chrome，在地址栏输入：`chrome://extensions` 并回车；
2. 在右上角开启「**开发者模式**」（Developer mode）；
3. 点击左上角「**加载已解压的扩展程序**」（Load unpacked）；
4. 选择本项目根目录下的 `dist-extension/chrome-mv3` 文件夹；
5. 加载完成后，核对扩展列表中显示的 ID 是否为 `oediboaeofmnlkgcjhnpfnngphkjooam`。

### Edge 浏览器：

1. 打开 Edge，在地址栏输入：`edge://extensions` 并回车；
2. 在左侧菜单栏开启「**开发人员模式**」开关；
3. 点击「**加载解压缩的扩展**」；
4. 选择 `dist-extension/chrome-mv3` 文件夹；
5. 确认 ID 与上述固定标识完全一致。

---

## 4. 注册 Native Messaging 静默唤起跳板

当 SheepGet 桌面端未运行时，扩展通过 Native Messaging Host 自动静默拉起桌面端主程序并获取本地通信端口。

### Windows 系统：

以普通用户权限运行 PowerShell 脚本：

```powershell
powershell -ExecutionPolicy Bypass -File scripts/register-host-windows.ps1
```

> 该脚本会自动生成 `com.sheepget.host.json` 并注册至当前用户的 Chrome / Edge NativeMessagingHosts 注册表路径。
> 卸载时执行：`powershell -File scripts/unregister-host-windows.ps1`。

### macOS / Linux 系统：

在终端执行注册脚本：

```bash
./scripts/register-host-posix.sh
```

> 该脚本会在 `~/Library/Application Support/Google/Chrome/NativeMessagingHosts/` 或 `~/.config/google-chrome/NativeMessagingHosts/` 下创建配置清单链接。

---

## 5. 扩展更新与重新加载流程

1. 每次修改扩展源码或更新版本后，运行构建：
   ```bash
   bun run --cwd extension build
   ```
2. 在 `chrome://extensions` 或 `edge://extensions` 页面中，找到 **SheepGet Integration Module**，点击右下角的「**重新加载**」（刷新旋转图标）按钮即可生效，无需重新选择文件夹。
