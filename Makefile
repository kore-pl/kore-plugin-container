# kore-plugin-container 构建入口：build / dist / check / clean。
#
# 位级可复现（dist）：同一源码在任意干净 clone、任意 umask 下重打包，sha256 必须一致。
# 手段：条目固定 + 全量归一（排序 / mtime / owner / group / mode）+ gzip -9n。
SHELL := /bin/bash

ID := container
BIN := bin/$(ID)
DIST := dist
VERSION := $(shell sed -n 's/.*"version"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' info.json | head -n 1)
PACKAGE := $(DIST)/$(ID)-$(VERSION).tar.gz

.PHONY: build dist check clean

# 静态构建：G-16 兼容性基线（CGO_ENABLED=0 + GOWORK=off + GOAMD64=v1 + trimpath + -s -w）。
build:
	CGO_ENABLED=0 GOWORK=off GOAMD64=v1 go build -trimpath -buildvcs=false -ldflags="-s -w" -o $(BIN) .

# 确定性打包：info.json（回填 size_bytes）+ bin/<id> + SHA256SUMS，显式列条目、无根 ./。
#
# 归一化清单（缺一条就可能位级漂移）：
#   1. 成员顺序：--sort=name
#   2. 时间戳：--mtime=@0
#   3. 属主：--owner=0 --group=0 --numeric-owner
#   4. 权限：stage 显式 chmod（info.json/SHA256SUMS 0644、bin 0755、目录 0755），
#      并用 tar --mode='u+rwX,go+rX' 二次归一——消除作者 umask 差异（077 → 0600 的包内 info.json 事故）
#   5. 压缩：gzip -9n（-n 不写原名与时间戳）
#   6. 构建环境：GOWORK=off 隔离外部 go.work，避免依赖解析漂移
#
# size_bytes 是**自指估算值**：它写在 info.json 里、info.json 又在包内，改这个数字会改包体。
# 且映射**非单调**——改 1 个数字位会同时改变 deflate 匹配与 SHA256SUMS 里的 info.json 摘要，
# gzip 长度实测跳变可达 -29..+32 字节（container 61 点窗口实测），单调迭代会跳过精确点。
# 因此策略：**先按固定顺序在 ±64 窗口搜精确不动点（声明 == 实测）**；找不到才回退 ±2 并告警。
# 收口值写回 info.json，故重复执行 dist 幂等：下一轮第一次打包即命中同一判决。
# 最终仍未满足 ±2 → exit 1，绝不静默接受。
dist: build
	@set -eo pipefail; \
	rm -rf $(DIST)/stage $(PACKAGE) $(PACKAGE).sha256 $(DIST)/info.json.next; \
	mkdir -p $(DIST)/stage/bin; \
	cp $(BIN) $(DIST)/stage/bin/$(ID); \
	python3 -c 'import json,sys; i=json.load(open("info.json")); m=json.load(open("manifest.json")); ks=("id","version","protocol","requires_kore","capabilities"); bad=[k for k in ks if i.get(k)!=m.get(k)]; sys.exit("manifest.json 与 info.json 不一致: "+",".join(bad)) if bad else None'; \
	mkpkg() { \
		sed "s/\"size_bytes\": [0-9]*/\"size_bytes\": $$1/" info.json > $(DIST)/stage/info.json; \
		( cd $(DIST)/stage && sha256sum info.json bin/$(ID) > SHA256SUMS ); \
		chmod 0755 $(DIST)/stage $(DIST)/stage/bin; \
		chmod 0755 $(DIST)/stage/bin/$(ID); \
		chmod 0644 $(DIST)/stage/info.json $(DIST)/stage/SHA256SUMS; \
		tar --sort=name --mtime=@0 --owner=0 --group=0 --numeric-owner --mode='u+rwX,go+rX' \
			-cf - -C $(DIST)/stage info.json bin SHA256SUMS | gzip -9n > $(PACKAGE); \
		stat -c%s $(PACKAGE); \
	}; \
	x=$$(sed -n 's/.*"size_bytes"[[:space:]]*:[[:space:]]*\([0-9]*\).*/\1/p' info.json | head -n 1); \
	if [ -z "$$x" ]; then x=0; fi; \
	chosen=""; \
	for cand in $$(seq $$(( x - 64 )) $$(( x + 64 ))); do \
		if [ "$$cand" -lt 1 ]; then continue; fi; \
		v=$$(mkpkg $$cand); \
		if [ "$$v" = "$$cand" ]; then chosen=$$cand; break; fi; \
	done; \
	if [ -z "$$chosen" ]; then \
		echo "[WARN] 无精确不动点，回退 ±2 估算值（size_bytes 仅供预检，权威尺寸以 release 资产实测为准）" >&2; \
		best=""; bestd=99; \
		for cand in $$(seq $$(( x - 64 )) $$(( x + 64 ))); do \
			if [ "$$cand" -lt 1 ]; then continue; fi; \
			v=$$(mkpkg $$cand); \
			d=$$(( v - cand )); if [ "$$d" -lt 0 ]; then d=$$(( 0 - d )); fi; \
			if [ "$$d" -le 2 ] && [ "$$d" -lt "$$bestd" ]; then best=$$cand; bestd=$$d; fi; \
		done; \
		chosen=$$best; \
	fi; \
	if [ -z "$$chosen" ]; then \
		echo "[FAIL] size_bytes 在 ±24 字节窗口内无 ≤2 固定点，拒绝出包" >&2; \
		exit 1; \
	fi; \
	actual=$$(mkpkg $$chosen); \
	sed "s/\"size_bytes\": [0-9]*/\"size_bytes\": $$chosen/" info.json > $(DIST)/info.json.next; \
	mv $(DIST)/info.json.next info.json; \
	declared=$$(tar -xOzf $(PACKAGE) info.json | sed -n 's/.*"size_bytes"[[:space:]]*:[[:space:]]*\([0-9]*\).*/\1/p'); \
	if [ "$$declared" != "$$chosen" ]; then \
		echo "[FAIL] 包内 size_bytes=$$declared 与收口值 $$chosen 不一致" >&2; \
		exit 1; \
	fi; \
	delta=$$(( actual - declared )); if [ "$$delta" -lt 0 ]; then delta=$$(( 0 - delta )); fi; \
	if [ "$$delta" -gt 2 ]; then \
		echo "[FAIL] size_bytes 收敛失败：包内=$$declared 实际=$$actual 偏差=$$delta > 2" >&2; \
		exit 1; \
	fi; \
	( cd $(DIST) && sha256sum $(ID)-$(VERSION).tar.gz > $(ID)-$(VERSION).tar.gz.sha256 ); \
	rm -rf $(DIST)/stage $(DIST)/info.json.next; \
	echo "打包完成: $(PACKAGE) sha256=$$(cut -d' ' -f1 $(DIST)/$(ID)-$(VERSION).tar.gz.sha256) size_bytes=$$declared 实际=$$actual"

# 门禁：gofmt（有输出即失败）→ go vet → go test。
check:
	@out="$$(gofmt -l .)"; if [ -n "$$out" ]; then echo "gofmt 需要格式化："; echo "$$out"; exit 1; fi; echo "gofmt: clean"
	GOWORK=off go vet ./...
	GOWORK=off go test -timeout 200s ./...

clean:
	rm -rf bin dist
	rm -f go.work.sum
