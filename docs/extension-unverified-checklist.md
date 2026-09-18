# 浏览器扩展架构与集成「真实环境待验证清单」

本项目秉持“能编译不等于能运行”、“不把未测试平台写为已通过”的工程质量铁律。下述各项在当前架构设计阶段已完成逻辑论证与规范推导，但受限于浏览器真实沙箱、跨平台系统行为及外部宿主限制，**必须在具备真实 Chrome / Edge 浏览器及多平台实机环境下进行实测闭环**：

---

### 1. 扩展固定标识（Extension ID）与跨环境侧载一致性
- **验证目标**：
  验证 `wxt.config.ts` 中配置的固定公钥派生 `key`，在用户将 `dist-extension/` 拷贝到任意不同目录、不同操作系统并在 Chrome 和 Edge 开发者模式加载时，生成的 Extension ID 是否在所有环境下**绝对恒定（100% 相同）**。
- **潜在风险与退路**：
  若不同浏览器或不同版本 Chrome 对 manifest 中的 `key` 派生规则存在细微差异，可能导致 Native Messaging Host 的 `allowed_origins` 匹配失败。需在真实 Chrome 120+ 及 Edge 120+ 上交叉实测。

---

### 2. 真实下载暂缓（`pause`）与回退恢复（`resume`）的内核边界
- **验证目标**：
  在真实网络环境下测试各类型资源下载触发时的原位 `pause` 与 `resume` 表现：
  1. 普通静态大文件（带 Content-Length）；
  2. 动态流式传输文件（Transfer-Encoding: chunked，无预知大小）；
  3. POST 表单触发下载（验证原位 pause 再 resume 是否不会重新发起二次 POST 请求）；
  4. 一次性防盗链下载链接。
- **关注点**：
  验证当桌面端断连或未启动且超时（2.5 秒）触发 `resume` 时，浏览器原生下载进度条是否能平滑继续，无损坏、无网络断开提示（`NETWORK_FAILED`）。

---

### 3. Native Messaging Host 静默唤起在三平台的系统兼容性
- **验证目标**：
  验证桌面端在**未运行状态**下，扩展通过 Native Messaging 发送消息时，操作系统拉起 `sheepget-host` 并由其拉起 SheepGet 主程序的全流程：
  - **Windows**：注册表 `HKCU\Software\Google\Chrome\NativeMessagingHosts` 与 `HKCU\Software\Microsoft\Edge\NativeMessagingHosts`；
  - **macOS**：`~/Library/Application Support/Google/Chrome/NativeMessagingHosts/` 清单；
  - **Linux**：`~/.config/google-chrome/NativeMessagingHosts/` 清单。
- **关注点**：
  验证主程序被拉起时，是否能静默初始化托盘并直接唤起独立文件信息窗口，而不会突兀弹出主界面；验证带空格安装路径与权限边界。

---

### 4. Background Service Worker 30 秒休眠唤醒与拦截时序
- **验证目标**：
  在 Chrome 打开 `chrome://serviceworker-internals`，手动点击 `Stop` 强制将 SheepGet 扩展的 Background Service Worker 杀掉（模拟休眠 30 秒状态）。
- **关注点**：
  在 SW 休眠瞬间，在页面点击一个目标接管后缀链接，观察浏览器内核唤醒 SW 的毫秒级延迟；确认唤醒后优先读取 `chrome.storage.local` 是否能立即命中接管，是否存在因唤醒延迟导致的漏接管现象。

---

### 5. 按住快捷键（Del / Ins 等）在复杂 DOM 与特权页面的降级表现
- **验证目标**：
  1. 普通网页中按住 Delete 触发下载，验证是否 100% 交由浏览器原生下载；按住 Insert 触发，验证是否忽略后缀强制接管；
  2. 验证页面失焦（Alt+Tab 切换窗口）、Tab 标签切换后，按键状态是否及时清零，无残留卡死；
  3. 在浏览器特权页面（如 `chrome://downloads`、Edge 设置页）及 Web Store 页面中点击下载链接，验证无法注入 Content Script 时的自动优雅降级（按全局配置接管，不崩溃）。

---

### 6. 媒体悬浮条在主流视频站点的定位与视口变化
- **验证目标**：
  在 Bilibili、YouTube 及普通 HTML5 `<video>` 页面上验证：
  1. 悬浮条是否精确贴近播放器右上角（避开全屏控制栏）；
  2. 页面滚动导致播放器移出视口时，悬浮条是否及时隐藏；滚回视口时是否准确定位恢复；
  3. 多视频同屏（如多个短视频卡片）时，各悬浮条是否各自独立管理；
  4. 手动关闭某个播放器的悬浮条后，刷新页面前是否不再出现，且其他播放器不受影响；
  5. 点击下载打开文件信息窗口后若用户取消，悬浮条是否仍保持可用可点击。
