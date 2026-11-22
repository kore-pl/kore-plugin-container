# kore-plugin-container

容器管理插件（**基础级骨架**）：Docker/Podman 面板的方法面。本仓只交付协议骨架与参数校验，**管理用户已有的容器运行时，内核零依赖，不内置 docker/podman 任何容器二进制**（G-05 无中间件、内核零容器运行时依赖；提案 §2.1 批3）。

换句话说：**本插件不启动、不连接、不捆绑任何容器运行时**。容器由使用者自行安装与管理；插件只提供"列容器 / 查详情"的方法协议骨架，返回结构化占位结果。真正的 daemon 调用属基础级边界用例，延至 v1.1，且不得标"生产级 / 开箱可用"。

## 协议面

- `kore.plugin.v1`，`lifecycle=lazy`，`depth_level=basic`
- 能力声明：`system:read`
- 方法：`container.list` / `container.inspect`（同时接受裸 `list` / `inspect`）
- 握手的 stdout 留给协议帧，日志一律写 stderr

## 标识校验（骨架口径）

`container.inspect` 的 `id` 字段接受两种形状：

- 完整 ID：64 位小写十六进制（如 64 个 `a-f0-9`），判定 `id_kind=id64`
- 名称 / 短标识：1–128 位 `[a-zA-Z0-9][a-zA-Z0-9_.-]*`，判定 `id_kind=name`
- 拒绝空值、空白、路径穿越字符（`/`、`..`）、NUL 与非法符号，返回 `E_INVALID`

校验只做字符串形状检查，**不执行任何运行时调用、不连 daemon**。结果中 `managed=false` / `applied=false` 明确告知调用方：骨架未连接任何真实容器运行时。

## 故障注入（基础级）

`tests/fault/` 覆盖 kill -9 与超时两类基础故障，断言方法级返回结构化错误，且 `Error.Code` 与故障类别对应：

| 故障 | 文件 | 期望错误码 |
| --- | --- | --- |
| kill -9 | `kill_test.go` | `E_UPSTREAM` |
| 超时 | `timeout_test.go` | `E_UPSTREAM` |

测试中的 `kore.shutdown` 请求均**配对读取唯一响应帧**并校验 id / req_id 回显与 `{"ok":true}` 结果，证明优雅退出真的回了帧，而非 net.Pipe 写阻塞的假通过。

## 构建与验收

```bash
gofmt -l . && go vet ./... && go test -timeout 200s ./...
make build && make dist
tar -tzf dist/container-0.1.0.tar.gz | head -1   # info.json
```

`dist` 使用确定性参数归档（`--sort=name --mtime=@0 --owner=0 --group=0 --numeric-owner` + `gzip -9n` + chmod 归一），重复构建 sha256 不变。`go:embed` 源是静态 `manifest.json`（不含 `size_bytes`），因此包内 `info.json` 的 `size_bytes` 回填不会改变二进制字节，单次回填即精确收敛。
