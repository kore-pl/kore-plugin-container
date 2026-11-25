# 配置 — 容器管理插件

## 能力声明（`info.json`）

- 协议：`kore.plugin.v1`；生命周期：`lazy`；深度等级见仓库 `info.json`。
- 能力：以 `info.json.capabilities` 与 `method_capabilities` 为准；读方法 `system:read`，写方法 `system:admin`。

## 配置优先级与存储

统一配置契约遵循 `kore/docs/PLUGIN-CONFIG.md`：`config.json` > 环境变量 > 代码默认值；配置文件位于 `<KORE_STATE_DIR>/config.json`，0600 原子写（临时文件 + fsync + rename）。保存后同一进程下一次调用立即生效（`restart_required=false`）；非法值返回 `E_INVALID` 且旧配置不变。

## 本插件可配项

容器管理全部通过固定 argv 的 `docker` 子进程完成，不经 shell；受保护容器名单不可被停止/删除；`container.update` 只调整资源限额；`stats` 是一次性采样，不起 ticker、不轮询（G-19）。配置契约 `container.config.get` / `container.config.set` 定义三个真实可配项：`docker_bin`、`protected_containers`、`docker_timeout_ms`，均为中文 label/help/default，优先级 `config.json` > 环境变量 > 代码默认值，保存后热生效（`restart_required=false`）。

## 状态目录

状态目录由环境变量指定（各插件不同，如 `KORE_APP_DEPLOY_STATE`、`KORE_STATE_DIR`），缺省一般位于 `<root>/state`。目录 0700、文件 0600；记录损坏或版本高于支持版本一律报错，不静默重置。
