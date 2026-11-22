# Changelog

## 0.1.0 (2026-11-22)

- feat: 新建 container 插件骨架，复用 kore.plugin.v1 握手与会话循环
- feat: container.list / container.inspect 方法级参数校验与结构化结果，容器 ID 支持 64hex 与名称两种形状，不真连 docker/podman daemon（G-05 / 提案 §2.1 批3）
- feat: 基础级故障注入测试（kill -9 / 超时），验证方法级错误返回；shutdown 请求配对读响应帧
- build: 确定性打包（--sort=name --mtime=@0 --owner=0 --group=0 --numeric-owner + gzip -9n + chmod 归一）
- build: go:embed 源改为静态 manifest.json（不含 size_bytes），消除打包自指震荡
