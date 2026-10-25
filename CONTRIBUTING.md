# 贡献指南

欢迎参与 pointcloud-twin，提交前请先阅读 [docs/requirements.md](requirements.md)（需求范围）与 [docs/design.md](docs/design.md)（技术方案）。

## Issue

- 提交前先搜索现有 Issue，确认没有重复。
- Bug 请给出：环境信息、版本、复现步骤、预期与实际结果、关键错误文字（截图不能替代文字）。
- 功能建议以 `Proposal:` 前缀命名，说明背景与期望。

## Pull Request

- **一个 PR 只包含一类修改**；在 `master` 上开发请先切出特性分支（`feat/xxx`、`fix/xxx`、`docs/xxx`）。
- 提交前充分自测：
  - Go：`go build ./... && go vet ./... && go test ./...`
  - 前端：`cd web && pnpm install && pnpm test && pnpm build`
- PR 描述写清：解决的问题（关联 `close #N`）、方案要点、自测过程；涉及 UI 的改动附前后截图。
- 提交信息使用中文 + Conventional Commits（如 `feat: 支持批量多站点转换`）。

## 开发速览

```bash
make build      # 构建 bin/pc2twin
make test       # Go 测试
make web-test   # 前端测试
make demo       # 示例数据转换 + 启动模拟数据源
```
