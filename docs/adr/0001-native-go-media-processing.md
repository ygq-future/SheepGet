---
status: accepted
---

# 以 Native Go 后端实现统一媒体处理层

SheepGet 优先保持轻量并覆盖常见下载媒体场景，采用统一的 Media Processing / Muxing 层，默认后端为 Native Go Backend。媒体处理优先通过 Go 原生实现或成熟 Go 媒体库完成，以明确的兼容范围换取可控的程序体积和核心复杂度。

## 职责边界

`HLS Engine → Media Processor → Output`

- HLS Engine：负责 playlist、segment、加密、下载和媒体信息组织。
- Media Processor：统一承接媒体处理，负责 TS/fMP4 解析、音视频轨合并、demux、mux、无损 remux 及 MP4 输出。
- Native Go Backend：默认实现，将具体 Go 媒体库封装在后端内；HLS Engine 和任务系统不直接依赖具体库。

## 支持范围

第一版优先覆盖常见 HLS / m3u8、MPEG-TS、fMP4 及 H.264 / H.265 + AAC 等组合。核心目标是正确组装和无损重新封装，不做重新编码；暂未支持的 codec、封装或复杂媒体结构明确提示“不支持”，避免为少数场景扩大核心复杂度。具体库及兼容矩阵需要在技术阶段验证，本决策不代表已有实现通过验证。

## 分发与扩展

FFmpeg 不是第一版的必需依赖，程序不默认打包完整 FFmpeg runtime。统一媒体处理边界保留可替换后端能力，未来可新增 FFmpeg Backend，通过用户指定 ffmpeg、可选组件安装或其他动态方式接入，无需重构 HLS Engine 和任务系统。

该选择意味着首期需要承担所支持格式的组装、时间戳及音视频轨兼容性验证；未来后端接入方式不作为首期交付要求。
