# kore-plugin-container

kore 的容器管理插件：**通过宿主机已安装的 `docker` CLI 真实管理已有容器**，提供容器
列表、精简详情、日志、启停/重启与一次统计采样。

本插件**不引入 Docker SDK、不内置任何容器运行时二进制、不直连 daemon socket**
（G-05 / G-17）：只依赖 `kore/pkg/pluginapi` 与 Go 标准库，所有外部动作都是固定 argv
的 `docker` 子进程（不经 shell）。docker CLI 缺失、daemon 不可达或非 Linux 平台一律
返回 `E_UNSUPPORTED`，不静默降级、不伪造数据。

## 协议面

- `kore.plugin.v1`，`lifecycle=lazy`，`depth_level=basic`（核心路径可用；边界用例延 v1.1，非功能降级）
- 能力声明：`system:read` + `system:admin`（启停/重启/镜像写/资源调整属于写操作）
- 方法（12 个业务方法）：`container.list` / `container.inspect` / `container.logs` /
  `container.stats` / `container.health` / `container.images` / `container.pull` /
  `container.rmi` / `container.update` / `container.start` / `container.stop` / `container.restart`
- `container.exec` / `shell` / `attach` **明确不提供**：命中即在触达 docker 前返回 `E_UNSUPPORTED`（低权插件进程执行容器内命令等同绕过权限模型，ADR-0018）
- 方法名必须带 `container.` 域前缀（协议 §7.2），裸 `list` 不再接受
- 握手的 stdout 留给协议帧，日志一律写 stderr

## 方法

| 方法 | 入参 | 结果 |
| --- | --- | --- |
| `container.list` | `{all?}` | `{containers[],count,all}` |
| `container.inspect` | `{id}` | `{id,name,image,state{},config{},network{},mounts[],resources{},health}` |
| `container.logs` | `{id,tail?}` | `{id,lines[],truncated}` |
| `container.start` | `{id}` | `{id,action:"start",ok,state}` |
| `container.stop` | `{id,confirm:true,timeout?}` | `{id,action:"stop",ok,state}` |
| `container.restart` | `{id,confirm:true,timeout?}` | `{id,action:"restart",ok,state}` |
| `container.stats` | `{id}` | `{id,name,cpu_percent,memory{},network{},block{},pids}` |
| `container.health` | `{id}` | `{id,status,health,running,restart_count,started_at}`（inspect 的 state/health 投影） |
| `container.images` | `{}` | `{images:[{id,repository,tag,size,created,dangling}],count}` |
| `container.pull` | `{ref,confirm:true}` | `{ref,ok,message,truncated}`（镜像引用过白名单） |
| `container.rmi` | `{ref,confirm:true,force?}` | `{ref,ok,force}`（必须显式确认） |
| `container.update` | `{id,memory_bytes?,cpus?,confirm:true}` | `{id,ok,memory_bytes?,cpus?}`；内存与 CPU 至少给一个 |

写方法（`start` / `stop` / `restart` / `pull` / `rmi` / `update`）里，除 `start` 外都要求 `confirm=true`；镜像引用与容器 ID 走同一套正则白名单，非法形状在触达 docker 前返回 `E_INVALID`。

### container.list

底层执行 `docker ps --format '{{json .}}'`（`all=true` 时追加 `--all`），逐条解析为：

```json
{"id":"短 ID(12)","name":"名称","image":"镜像","state":"running",
 "status":"Up 2 minutes","ports":"0.0.0.0:18081->80/tcp",
 "created":"2026-11-23T03:32:31+08:00","running_for":"2 minutes ago"}
```

`created` 由 docker 的 `CreatedAt` 归一为 RFC3339；解析失败时保留原串，不编造时间。
`all` 回显本次查询范围；`count` 等于 `len(containers)`。

### container.inspect

底层执行 `docker inspect <id>`（数组输出的第一个元素），只保留关键字段：

- `state{status,running,started_at,restart_count}`（`restart_count` 取自顶层 `RestartCount`）
- `config{env,cmd,labels}`：`env` 已按名称脱敏（见下）
- `network{ip,ports}`：`ip` 优先取网络名排序后的首个非空地址；`ports` 是
  `{container_port,protocol,host_ip,host_port}` 数组，未发布的端口 `host_*` 为空串
- `mounts[]`：`{type,source,destination,mode,rw}`
- `resources{memory_limit,cpu_quota}`：字节 / `CpuQuota` 原值
- `health`：`State.Health.Status`；未配置健康检查时为空串

### container.logs

底层执行 `docker logs --tail N <id>`，**合并 stdout+stderr**：

- `tail` 缺省 `100`，范围 `1..2000`
- 单行 ≤ **8 KiB**，超长截断并置 `truncated=true`（UTF-8 边界安全）
- 单次采集总预算 2 MiB，超出丢弃并置 `truncated=true`，不阻塞 docker 子进程
- 行内容原样返回，不做转义或推断

### container.start / stop / restart

- `start` 不需要确认；`stop` / `restart` **必须传 `confirm=true`**，否则返回
  `E_INVALID` 并说明，且在触达 docker 之前就拒绝
- `timeout` 仅对 stop/restart 生效：缺省交给 docker（10s），范围 `0..300` 秒
- 动作成功后返回完整 ID 与尽力获取的 `state`；容器已被 `--rm` 删除时 `state` 为空串，
  但动作本身仍报告 `ok=true`（不把成功改判为失败）

### container.stats

底层执行 `docker stats --no-stream --format '{{json .}}' <id>`，把人类可读值换算为字节：

- `cpu_percent`：`0.37%` → `0.37`
- `memory{usage,limit,percent}`：`118.4MiB / 1GiB` → 字节
- `network{input,output}`：`5.43MB / 477kB` → 字节
- `block{read,write}`：`680kB / 6.05MB` → 字节
- `pids`：容器内进程数
- 容器没有实时统计（如已停止）时返回 `E_CONFLICT`

## 安全护栏

| 护栏 | 行为 |
| --- | --- |
| 容器标识正则 | 必须匹配 `^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$`；空白、`/`、`;`、`$()`、引号、`-` 开头等注入形状在触达 docker 前返回 `E_INVALID` |
| 固定 argv | 所有调用都是 `exec.Command(docker, args...)`，不经 shell；标识经正则校验后作为独立 argv 传入 |
| 保护名单 | `KORE_PROTECTED_CONTAINERS`（逗号分隔名称 / 完整 ID / ≥6 位十六进制短 ID）+ 自动探测的宿主自身容器 ID；命中时 start/stop/restart 返回 `E_UNAUTHORIZED` |
| 二次确认 | stop/restart 缺 `confirm=true` → `E_INVALID`，不执行任何 docker 命令 |
| env 脱敏 | 名称含 `PASS` / `TOKEN` / `SECRET` / `KEY`（大小写不敏感）的环境变量值替换为 `***`，变量名保留 |
| 命令超时 | 单条 docker 命令 20s，超时终止并返回 `E_UPSTREAM` |
| 平台/依赖缺失 | 非 Linux、找不到 docker CLI、daemon 不可达 → `E_UNSUPPORTED` + 可操作说明 |
| 权限不足 | 无 docker.sock 访问权 → `E_NEED_ROOT` + 提示经内核提权通道（`root_actions`）授权（ADR-0018 降级矩阵）；不引导加入 docker 组 |
| 资源 | 无后台 goroutine / ticker；每次请求按需起子进程，结束即回收 |

保护名单默认会把「承载插件自身的容器」纳入：从 `/proc/self/cgroup` 提取 64 位容器 ID，
并在 `HOSTNAME` 为 12/64 位十六进制时一并纳入。插件跑在宿主上时不猜测、不误伤。

## 环境变量

| 变量 | 缺省 | 说明 |
| --- | --- | --- |
| `KORE_DOCKER_BIN` | `docker` | docker CLI 名或绝对路径（走 `LookPath`） |
| `KORE_PROTECTED_CONTAINERS` | 空 | 逗号分隔保护名单；非法标识启动即报错 |
| `KORE_LOG_LEVEL` | `warn` | `debug/info/warn/error` |

`DOCKER_HOST` / `DOCKER_CONTEXT` / `DOCKER_CONFIG` 等 docker 自身环境变量照常继承。

## 配置面板

插件实现 `container.config.get` / `container.config.set`（PLUGIN-CONFIG 契约），统一配置面板可渲染独立设置表单。
可配置项：`docker_bin`（Docker 命令行路径）、`protected_containers`（保护容器名单）、`docker_timeout_ms`（命令超时）。
保存后写入 `<KORE_STATE_DIR>/config.json`（0600、原子写），同进程下一次方法调用即用新值，无需重启 kored。
优先级：`config.json` > 环境变量 > 代码默认值；界面保存后以文件为准。老用户的 `KORE_DOCKER_BIN` / `KORE_PROTECTED_CONTAINERS` 继续生效。

## 错误映射

| 执行面情况 | 协议错误码 |
| --- | --- |
| 参数形状 / ID 非法 / 缺 confirm / tail 或 timeout 越界 | `E_INVALID` |
| 容器不存在（docker `No such container`） | `E_NOTFOUND` |
| 命中保护名单 | `E_UNAUTHORIZED` |
| 非 Linux / 无 docker CLI / daemon 不可达 | `E_UNSUPPORTED` |
| 无 docker.sock 访问权（需宿主 root 授权） | `E_NEED_ROOT`（`detail.reason=need_root`） |
| docker 超时 / 其它 CLI 失败 | `E_UPSTREAM` |
| 停止的容器读 stats | `E_CONFLICT` |
| 结果序列化失败 | `E_INTERNAL` |

## 测试

```bash
gofmt -l .
go vet ./...
go test -timeout 200s ./...
```

- `internal/containerop`：ID 正则与注入拒绝、保护名单命中/误伤、confirm 必填、
  Go 平台 E_UNSUPPORTED、docker 缺失、`docker ps --format json` fixture 解析、
  `docker inspect` fixture（端口/网络/挂载/资源/health）、env 脱敏、logs 单行 8 KiB
  截断、stats 人类可读值换算、tail/timeout 边界。
- `tests/fault/`：四类故障注入（kill / timeout / disk / cgroup）覆盖全部 12 个业务方法，
  断言 `E_UPSTREAM` / `E_INTERNAL` / `E_UNSUPPORTED` 与无故障 sanity。
- `tests/e2e/`：真实 docker 全流程，默认跳过：

```bash
KORE_CONTAINER_E2E=1 go test -timeout 180s -v ./tests/e2e
```

  步骤：起 `kore-ct-test`（`nginx:alpine`，映射 `18081:80`，带 `APP_PASSWORD`）→
  list 可见 → inspect 状态/端口/env 脱敏 → logs 含 nginx 启动日志 → stats 有
  CPU/内存 → 缺 confirm 的 stop 被拒且容器仍 running → stop 后 exited →
  start 恢复 running → restart 后 `started_at` 变化 → 注入 ID 被拒 →
  第二个实例用保护名单拒绝 start/stop/restart → shutdown → `docker rm -f` 后
  `docker ps -a` 不再包含 `kore-ct-test`。测试输出 `E2E_*` 证据行。

## 构建与打包

```bash
make build && make dist
tar -tzf dist/container-0.6.0.tar.gz
```

静态构建基线：`CGO_ENABLED=0 GOWORK=off GOAMD64=v1 -trimpath -buildvcs=false
-ldflags="-s -w"`。`dist` 显式列出 `info.json`、`bin/container`、`SHA256SUMS`，用
`--sort=name --mtime=@0 --owner=0 --group=0 --numeric-owner` + `gzip -9n` + chmod
归一保证位级可复现；包内 `info.json` 的 `size_bytes` 回填为真实包体字节数，连续两次
`make dist` 的 sha256 逐字一致。`go:embed` 源是静态 `manifest.json`（不含
`size_bytes`），因此回填不会改变二进制字节。

MIT License — pengtang1024

<!-- oss:begin -->
## 开源与贡献

[![CI](https://github.com/kore-pl/kore-plugin-container/actions/workflows/ci.yml/badge.svg)](https://github.com/kore-pl/kore-plugin-container/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Go 1.26](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white)](go.mod)

本仓库是 **kore** 的 `容器管理插件`（id `container`），独立仓库、独立版本线、独立 CI（少触发：push main / tag / PR / 手动）。内核与协议见 [kore](https://github.com/kore-pl/kore)。

- **许可**：MIT（inbound = outbound，不要求 CLA）；提交用 `git commit -s` 签 DCO。
- **贡献**：[CONTRIBUTING.md](CONTRIBUTING.md)（构建、门禁、发版、边界）。
- **安全**：[SECURITY.md](SECURITY.md)——漏洞不要开公开 issue。
- **行为准则**：[CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md)。
- **兼容性**：`protocol: kore.plugin.v1` · `requires_kore: >=0.1.0 <0.2.0` · `lifecycle: lazy` · 深度等级 `basic`。
- **验证方式**：本地 `make check` 优先（CI 只在 push main / tag / PR / 手动跑；发布前 `make dist` 连跑两次核对 sha 一致）。
- **发布产物**：`make dist` → `dist/container-0.6.0.tar.gz` + `.sha256`（位级可复现，连跑两次 sha 相同）。
- **相关仓库**：[索引 kore-plugins](https://github.com/kore-pl/kore-plugins) · [协议文档](https://github.com/kore-pl/kore/blob/main/docs/PLUGIN-PROTOCOL.md) · [SDK 指南](https://github.com/kore-pl/kore/blob/main/docs/PLUGIN-SDK.md)

<!-- oss:end -->
