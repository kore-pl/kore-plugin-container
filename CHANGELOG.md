# Changelog

## 0.5.5 (2026-11-24)

- chore(depth_level): `production` → `basic`，依据 `PLUGIN-CATALOG.md` §6.1 升级判据——升 `production` 要求
  ①边界用例覆盖（异常输入 / 并发 / 权限不足 / 资源耗尽各 ≥1 例并进 CI）与 ②故障注入通过**两条都满足**；
  本仓 `tests/fault/` 四类故障注入真实 PASS（②达成），但 `tests/boundary/` 不存在、并发用例
  （`t.Parallel`）0 命中，①无证据 → 原 `production` 属单维度达标的**过度声明**，索引的 `basic` 才是正确值
- chore: **不是降级缺陷**——零代码变更，执行面 / 拒绝清单 / 故障矩阵均未改；边界用例与高级配置延到 v1.1
- chore(release): 版本 bump 0.5.4 → 0.5.5（`info.json` 在 dist 包体内，改元数据必须 bump，RELEASE §1）

## 0.5.4 (2026-11-23)

- **行为变更**：`docker.sock` 无权访问时由 `E_UNSUPPORTED` 改为 **`E_NEED_ROOT`**（`detail.reason=need_root`），对齐 ADR-0018 降级矩阵（container × docker.sock）；message 提示「由管理员通过内核提权通道（`root_actions`）授权后重试」，仍不引导加入 docker 组（等价 root、绕过审计）
- fix: 权限分支前移到「daemon 不可达」之前——docker 无权访问 socket 的输出同时含 `permission denied` 与 socket 路径，原先会被 daemon 分支吞掉并误报成「daemon 没启动」
- 保持不变：非 Linux / 无 docker CLI / daemon 不可达仍 `E_UNSUPPORTED`；拒绝清单与逃逸参数仍 `E_UNSUPPORTED`；保护名单仍 `E_UNAUTHORIZED`
- 前端影响：可据 `code=E_NEED_ROOT` 区分「权限不够去授权」与「依赖缺失去装 docker」，不再靠 message 字符串判别

## 0.5.3 (2026-11-23)

- fix: 修正 docker 权限引导——不再建议加入 docker 组（与 ADR-0018 冲突）；socket 权限错误改为「请通过内核提权通道授权，或由管理员按最小权限原则自行配置」，代码注释写明 docker 组等价 root、会绕过提权审计

## 0.5.2 (2026-11-23)

- fix: `container.exec` 错误码语义修正——拒绝清单/逃逸参数 → `E_UNSUPPORTED`、root 未确认 → `E_INVALID`、docker 不可用 → `E_UNSUPPORTED`；三类统一带显式 `detail.reason`（`denylist` / `escape_flag` / `confirm_required` / `docker_missing`），前端不再靠 message 字符串判别

## 0.5.1 (2026-11-23)

- build(dist): 定位 `size_bytes` ±1/±2 根因——该字段写在 `info.json` 内、info.json 又在包内（**自指**），且改数字会同时改变 deflate 匹配与 `SHA256SUMS` 里的 info.json 摘要，映射**非单调**（61 点窗口实测 -29..+32 字节跳变）；`make dist` 改为**先搜精确不动点（±64 窗口，声明 == 实测）**，找不到才回退 ±2 并打 WARN（口径：声明值仅供预检，权威尺寸=release 资产实测）
- chore: 本仓示例——size_bytes 由 1527625 收敛到 **1527627**（与实测逐字相等）

## 0.5.0 (2026-11-23)

- feat: `container.exec` 一次性命令执行——固定 argv、不经 shell；`timeout_ms` 缺省/越界夹取到 **30s** 并标 `timeout_clamped`；stdout/stderr 各截断 **256 KiB**（标 `truncated`）；默认容器内**非 root**（`user` 可显式指定），root 需 `root:true` + `confirm:true`；内置拒绝清单与逃逸参数拦截
- test: argv 透传 / 超时夹取 / 输出截断 / 拒绝清单与 confirm 门禁

## 0.4.1 (2026-11-23)

### Changed

- 提升为 `production` 深度等级（`depth_level`），故障注入测试已覆盖 6 个场景

## 0.4.0 (2026-11-23)

- feat: `info.json` 新增方法级 `method_capabilities`（12 条）：读方法 `list` / `inspect` /
  `logs` / `stats` / `images` / `health` → `system:read`；写方法 `start` / `stop` /
  `restart` / `pull` / `rmi` / `update` → `system:admin`。此前只有插件级 capability，
  内核按方法授权与审计的粒度不足（§3.5 #12）
- feat: `container.images`（只读列镜像，`docker images --format '{{json .}}'`，含 `dangling` 标记）
- feat: `container.health`（投影 inspect 已有的 `state`/`health` 字段，不额外起 docker 子进程）
- feat: `container.update`（`docker update --memory/--cpus` 改资源限额，至少给一项；
  必须 `confirm=true`；写前解析容器真实身份并过保护名单）
- feat: `container.pull` / `container.rmi` 显式镜像管理；`pull` 只由用户显式触发
  （`confirm=true`），插件不做后台拉取、无 ticker；输出受 64 KiB 预算限制
- security: **明确不做 `exec` / `shell` / `attach`**——插件进程按 ADR-0018 保持低权
  （真 root 部署下降权到 `kore-<id>`），容器内命令执行等同绕过插件权限模型，且容器
  编排属 G-11 不做项。调用 `container.exec` 等方法在触达 docker 前返回
  `E_UNSUPPORTED` 并说明理由；没有白名单命令、没有交互式入口、没有输出采集面
- security: 镜像引用形状校验（首字符字母/数字，仅字母数字与 `. _ / : @ -`，上限 255）
  在触达 docker 前拒绝空白、引号、`$()` 与 `-` 开头的 flag 注入；`rmi` 被运行中容器
  引用时映射 `E_CONFLICT`，拉取鉴权失败映射 `E_UNAUTHORIZED`
- test: `info.json` 的 `method_capabilities` 与代码方法表逐条一致性表驱动测试（含
  capability 分类与「无遗漏 / 无多余」）；镜像解析、`pull`/`rmi`/`update` 参数与确认、
  保护名单拒绝、`health` 投影、`exec`/`shell` 拒绝用例
- docs: README 补齐新方法、安全护栏与权限矩阵；`logs` 注释里的「缺省 100」更正为 200

## 0.3.0 (2026-11-23)

- feat: `container.logs` 新增可选 `since`（原样作为 `docker logs --since` 的 argv 元素，
  支持 Go 时长 / Unix 秒 / RFC3339，形状校验拒绝空白等明显误传），`tail` 缺省 100 → 200
- feat: `container.list` 每条新增 `protected` 布尔：命中保护名单的容器由前端展示锁标并禁用启停
- feat: 保护名单短 ID 改为 ≥6 位十六进制**双向**前缀匹配——名单给完整 ID、`docker ps` 只
  拿到 12 位短 ID 时同样命中
- feat: `container.inspect` 资源限额新增 `restart_policy`（docker HostConfig.RestartPolicy.Name）
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
