# Changelog

## 0.3.0 (2026-11-23)

- feat: `container.logs` 新增可选 `since`（原样作为 `docker logs --since` 的 argv 元素，
  支持 Go 时长 / Unix 秒 / RFC3339，形状校验拒绝空白等明显误传），`tail` 缺省 100 → 200
- feat: `container.list` 每条新增 `protected` 布尔：命中保护名单的容器由前端展示锁标并禁用启停
- feat: 保护名单短 ID 改为 ≥6 位十六进制**双向**前缀匹配——名单给完整 ID、`docker ps` 只
  拿到 12 位短 ID 时同样命中
- test: `TestProtectedListFlag`（stop common-mysql 被 E_UNAUTHORIZED 拒绝 + list 标记）、
  `since` 形状校验、反向短 ID 前缀命中


## 0.2.0 (2026-11-23)

- feat: 真实容器列表/详情/日志/启停/统计（去 noop）
- build: 位级可复现打包（stage chmod + tar --mode 归一、size_bytes 收敛），二进制改嵌 manifest.json，消除同版本 sha 漂移

## 0.1.0 (2026-11-23)

- feat: 新建 container 插件骨架，复用 kore.plugin.v1 握手与会话循环
- feat: `container.list` / `container.inspect` 从 noop 占位改为**真实 docker CLI**：
  `docker ps --format '{{json .}}'`（`all` 可选）解析为
  `{id,name,image,state,status,ports,created,running_for}`；
  `docker inspect` 收敛为 `{id,name,image,state,config,network,mounts,resources,health}`
  （`restart_count` 取顶层字段、端口数组化、网络 IP 确定性选取）
- feat: `container.logs`（`docker logs --tail N`，stdout+stderr 合并，单行 ≤8 KiB、
  总量 ≤2 MiB、超限标记 `truncated`）、`container.start` / `container.stop` /
  `container.restart`（stop/restart 必须 `confirm=true`，`timeout` 0..300 秒）、
  `container.stats`（`docker stats --no-stream` 解析 CPU%/内存/网络 IO/块 IO/PIDs）
- security: 容器标识只接受 `^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$`，注入形状在触达
  docker 前返回 `E_INVALID`；所有调用固定 argv、不经 shell；单命令 20s 超时
- security: `KORE_PROTECTED_CONTAINERS` 保护名单（名称 / 完整 ID / ≥6 位十六进制
  短 ID）+ 自动探测宿主自身容器 ID，命中时 start/stop/restart 返回 `E_UNAUTHORIZED`
- security: inspect 的 env 名称含 `PASS/TOKEN/SECRET/KEY` 时值替换 `***`；
  非 Linux / 无 docker CLI / daemon 不可达 / 无 socket 权限返回 `E_UNSUPPORTED`
- refactor: 方法名按协议统一为 `<plugin-id>.<action>`，只接受 `container.*`；
  移除 `noopExecutor` 与 `managed` / `applied` 占位语义
- test: `internal/containerop` 覆盖 ID 正则与注入、保护名单、confirm 必填、ps/inspect
  fixture、env 脱敏、logs 单行截断、stats 换算；`tests/fault/` 扩为四类故障注入
  （kill / timeout / disk / cgroup）覆盖七个方法
- test: 新增 `tests/e2e/` 真实 docker 端到端（list → inspect → logs → stats →
  stop 缺 confirm 被拒 → stop/start/restart → 注入拒绝 → 保护名单拒绝 → 清理），
  默认跳过，`KORE_CONTAINER_E2E=1` 开启
- build: 确定性打包（--sort=name --mtime=@0 --owner=0 --group=0 --numeric-owner +
  gzip -9n + chmod 归一）
- build: go:embed 源改为静态 manifest.json（不含 size_bytes），消除打包自指震荡
