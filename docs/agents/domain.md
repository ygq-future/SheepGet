# Domain docs

## 布局与读取规则

采用单一上下文布局：

- CONTEXT.md：根目录领域术语表。
- docs/adr/：架构及重要决策记录。

探索项目前读取 CONTEXT.md，以及与当前工作相关的 ADR。
文件尚不存在时直接继续，无需预先创建或提示缺失。
grill-with-docs 调用 domain-modeling，在术语或决策确定后逐步创建。

## 术语与决策

涉及领域概念时，使用 CONTEXT.md 中约定的术语。
发现术语缺口时交由 domain-modeling 澄清。
建议与已有 ADR 冲突时，明确指出对应 ADR 及重新讨论的理由。
