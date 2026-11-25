# 使用方法 — 容器管理插件

## 协议面

协议 `kore.plugin.v1`，`lifecycle=lazy`，`depth_level=basic`，能力声明 `system:read` + `system:admin`。12 个业务方法：`list` / `inspect` / `logs` / `stats` / `health` / `images` / `pull` / `rmi` / `update` / `start` / `stop` / `restart`，方法名必须带 `container.` 域前缀。`container.exec` / `shell` / `attach` 明确不提供（ADR-0018），命中即在触达 docker 前返回 `E_UNSUPPORTED`。握手的 stdout 留给协议帧，日志一律写 stderr。

## 核心能力

容器管理全部通过固定 argv 的 `docker` 子进程完成，不经 shell；受保护容器名单不可被停止/删除；`container.update` 只调整资源限额；`stats` 是一次性采样，不起 ticker、不轮询（G-19）。配置契约 `container.config.get` / `container.config.set` 定义三个真实可配项：`docker_bin`、`protected_containers`、`docker_timeout_ms`，均为中文 label/help/default，优先级 `config.json` > 环境变量 > 代码默认值，保存后热生效（`restart_required=false`）。

## 典型流程

1. 握手：调用 `kore.ping` 确认插件存活（`kore.plugin.v1`）。
2. 读操作：调用本插件的只读方法（列表 / 详情 / 探测 / 查询），确认返回结构。
3. 写操作：确认 `info.json` 已声明对应能力，按方法要求提供显式确认参数（如 `confirm: true`）。
4. 失败处理：错误以结构化 `{"code","message"}` 返回；按 `code` 分流，不解析中文文案。

## 安全边界

拒绝清单与逃逸参数（exec / shell / attach）在触达 docker 之前即返回 `E_UNSUPPORTED`；受保护容器名单阻止误停生产容器；docker.sock 无权限时按 ADR-0018 返回 `E_NEED_ROOT` 并提示走内核提权通道，不引导加入 docker 组（等价 root）；非 Linux / 无 docker CLI / daemon 不可达仍返回 `E_UNSUPPORTED`。插件以独立进程运行，socket 权限 0600，绕过权限或连接他人 socket 属漏洞。
