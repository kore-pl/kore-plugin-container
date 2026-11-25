# 容器管理插件（kore-plugin-container）

kore-plugin-container 是 kore 的 `container` 插件，独立仓库、独立版本线、独立 CI。当前版本 `0.6.0`，协议 `kore.plugin.v1`。

## 这是什么

本插件通过宿主机已安装的 `docker` CLI 真实管理已有容器，提供列表、精简详情、日志、启停/重启与一次统计采样。不引入 Docker SDK、不内置容器运行时、不直连 daemon socket（G-05 / G-17）：所有外部动作都是固定 argv 的 `docker` 子进程（不经 shell）。docker CLI 缺失、daemon 不可达或非 Linux 平台一律返回 `E_UNSUPPORTED`，不静默降级、不伪造数据。

## 快速入口

- [安装](Installation)：插件包获取与安装步骤
- [配置](Configuration)：配置项、能力声明与环境变量
- [使用方法](Usage)：协议方法与典型调用
- [常见问题](FAQ)：边界、权限与降级行为
- [变更记录](Changelog)：语义化版本历史

## 相关链接

- 源码：https://github.com/kore-pl/kore-plugin-container
- 内核与协议：https://github.com/kore-pl/kore
- 索引：https://github.com/kore-pl/kore-plugins
