# Plugin de gerenciamento de contêineres（kore-plugin-container）

## Visão geral

Este plugin gerencia contêineres existentes pela CLI `docker` instalada no host: listagem, inspeção resumida, logs, iniciar/parar/reiniciar e uma amostragem de estatísticas. Não inclui Docker SDK, runtime embarcado nem acesso direto ao socket do daemon (G-05 / G-17): toda ação externa é um subprocesso `docker` com argv fixo (sem shell). CLI ausente, daemon inacessível ou plataforma não Linux retornam `E_UNSUPPORTED`, sem degradação silenciosa nem dados fabricados.

## Superfície do protocolo

Protocolo `kore.plugin.v1`, `lifecycle=lazy`, `depth_level=basic`, capacidades `system:read` + `system:admin`. Doze métodos de negócio: `list` / `inspect` / `logs` / `stats` / `health` / `images` / `pull` / `rmi` / `update` / `start` / `stop` / `restart`, todos com o prefixo de domínio `container.`. `container.exec` / `shell` / `attach` não são oferecidos explicitamente (ADR-0018) e retornam `E_UNSUPPORTED` antes de tocar no docker. O stdout do handshake é reservado para quadros; os logs vão para stderr.

## Funcionalidades principais

Todas as operações passam por subprocessos `docker` com argv fixo, nunca por shell; contêineres protegidos não podem ser parados ou excluídos; `container.update` apenas ajusta limites de recursos; `stats` é uma amostragem única, sem ticker nem polling (G-19). O contrato de configuração `container.config.get` / `container.config.set` define três opções reais: `docker_bin`, `protected_containers`, `docker_timeout_ms`, com rótulo/ajuda/padrão em chinês; a precedência é `config.json` > ambiente > valores do código, e salvar tem efeito imediato (`restart_required=false`).

## Limite de segurança

A lista de negação e os parâmetros de escape (exec / shell / attach) retornam `E_UNSUPPORTED` antes de chegar ao docker; a lista de contêineres protegidos evita paradas acidentais em produção; quando docker.sock está inacessível, retorna `E_NEED_ROOT` conforme ADR-0018 e aponta para o canal de elevação de privilégios do kernel, sem sugerir o grupo docker (equivalente a root); não Linux / sem CLI / daemon inacessível continuam retornando `E_UNSUPPORTED`. O plugin roda como processo isolado com socket 0600; burlar permissões ou conectar a outro socket é uma vulnerabilidade.

## Build e verificação

A verificação diária usa `make check` local (gofmt → go vet → go test -timeout 200s); antes do lançamento, `make dist` duas vezes com sha256 idêntico. GitHub Actions é disparado apenas manualmente (`workflow_dispatch`), enquanto Gitea Actions dispara por tag de versão. `depth_level=basic`: os caminhos principais funcionam; casos limite e injeção de falhas ficam para v1.1 — uma decisão de posicionamento, não uma degradação funcional.

## Links relacionados

- [源码](https://github.com/kore-pl/kore-plugin-container)
- [kore 内核与协议](https://github.com/kore-pl/kore)
- [插件索引](https://github.com/kore-pl/kore-plugins)
