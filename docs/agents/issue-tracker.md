# Issue tracker: GitHub

仓库：https://github.com/ygq-future/SheepGet
规格与任务保存在 GitHub Issues，使用 gh CLI 操作。
命令显式指定 --repo ygq-future/SheepGet，避免依赖本地 remote。

## 操作约定

- 发布规格或任务：创建 GitHub issue，每个任务独立一条。
- 获取任务：读取 issue 正文、标签及评论。
- 多行正文或评论：写入临时文件，使用 --body-file 提交。
- 分诊状态：使用 triage-labels.md 中的标签。
- 完成任务：记录结果并关闭 issue。

## Pull requests as a triage surface

PRs as a request surface: no.

## Wayfinding

- 地图使用带 wayfinder:map 标签的 issue。
- 决策任务关联为地图的 sub-issue，并标记 wayfinder:<type>。
- 不支持 sub-issue 时，地图使用任务列表，子任务注明 Part of #<map>。
- 阻塞关系使用 GitHub 原生 issue dependencies；
  不可用时在正文记录 Blocked by: #<number>。
- 按地图顺序选择未关闭、无未完成阻塞项且无人认领的任务。
- 认领时分配给执行者。
- 解决后记录答案、关闭任务，并将结论链接补入地图。
