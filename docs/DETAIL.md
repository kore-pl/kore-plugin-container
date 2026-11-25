# 容器管理插件（kore-plugin-container）

## 简介

本插件通过宿主机已安装的 `docker` CLI 真实管理已有容器，提供列表、精简详情、日志、启停/重启与一次统计采样。不引入 Docker SDK、不内置容器运行时、不直连 daemon socket（G-05 / G-17）：所有外部动作都是固定 argv 的 `docker` 子进程（不经 shell）。docker CLI 缺失、daemon 不可达或非 Linux 平台一律返回 `E_UNSUPPORTED`，不静默降级、不伪造数据。

## 协议面

协议 `kore.plugin.v1`，`lifecycle=lazy`，`depth_level=basic`，能力声明 `system:read` + `system:admin`。12 个业务方法：`list` / `inspect` / `logs` / `stats` / `health` / `images` / `pull` / `rmi` / `update` / `start` / `stop` / `restart`，方法名必须带 `container.` 域前缀。`container.exec` / `shell` / `attach` 明确不提供（ADR-0018），命中即在触达 docker 前返回 `E_UNSUPPORTED`。握手的 stdout 留给协议帧，日志一律写 stderr。

## 核心能力

容器管理全部通过固定 argv 的 `docker` 子进程完成，不经 shell；受保护容器名单不可被停止/删除；`container.update` 只调整资源限额；`stats` 是一次性采样，不起 ticker、不轮询（G-19）。配置契约 `container.config.get` / `container.config.set` 定义三个真实可配项：`docker_bin`、`protected_containers`、`docker_timeout_ms`，均为中文 label/help/default，优先级 `config.json` > 环境变量 > 代码默认值，保存后热生效（`restart_required=false`）。

## 安全边界

拒绝清单与逃逸参数（exec / shell / attach）在触达 docker 之前即返回 `E_UNSUPPORTED`；受保护容器名单阻止误停生产容器；docker.sock 无权限时按 ADR-0018 返回 `E_NEED_ROOT` 并提示走内核提权通道，不引导加入 docker 组（等价 root）；非 Linux / 无 docker CLI / daemon 不可达仍返回 `E_UNSUPPORTED`。插件以独立进程运行，socket 权限 0600，绕过权限或连接他人 socket 属漏洞。

## 构建与验证

日常验证用本地 `make check`（gofmt → go vet → go test -timeout 200s）；发布前 `make dist` 连跑两次核对 sha256 一致。GitHub Actions 只手动触发（`workflow_dispatch`），Gitea Actions 按版本 tag 触发。`depth_level=basic`：核心路径可用，边界用例与故障注入延 v1.1，属定位而非功能降级。

## 相关链接

- [源码](https://github.com/kore-pl/kore-plugin-container)
- [kore 内核与协议](https://github.com/kore-pl/kore)
- [插件索引](https://github.com/kore-pl/kore-plugins)
