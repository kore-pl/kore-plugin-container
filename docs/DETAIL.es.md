# Complemento de gestión de contenedores（kore-plugin-container）

## Resumen

Este complemento gestiona contenedores existentes mediante la CLI `docker` instalada en el host: listado, inspección concisa, registros, inicio/parada/reinicio y una muestra de estadísticas. No incluye Docker SDK, ni un runtime embebido, ni conexión directa al socket del daemon (G-05 / G-17): cada acción externa es un subproceso `docker` con argv fijo (sin shell). Si falta la CLI, el daemon no responde o la plataforma no es Linux, devuelve `E_UNSUPPORTED`, sin degradación silenciosa ni datos inventados.

## Superficie del protocolo

Protocolo `kore.plugin.v1`, `lifecycle=lazy`, `depth_level=basic`, capacidades `system:read` + `system:admin`. Doce métodos de negocio: `list` / `inspect` / `logs` / `stats` / `health` / `images` / `pull` / `rmi` / `update` / `start` / `stop` / `restart`, todos con el prefijo de dominio `container.`. `container.exec` / `shell` / `attach` no se ofrecen explícitamente (ADR-0018) y devuelven `E_UNSUPPORTED` antes de tocar docker. La salida estándar del handshake se reserva para tramas; los registros van a stderr.

## Funcionalidades principales

Todas las operaciones se realizan mediante subprocesos `docker` con argv fijo, nunca una shell; los contenedores protegidos no pueden detenerse ni borrarse; `container.update` solo ajusta límites de recursos; `stats` es una muestra única, sin ticker ni sondeo (G-19). El contrato de configuración `container.config.get` / `container.config.set` define tres opciones reales: `docker_bin`, `protected_containers`, `docker_timeout_ms`, con etiqueta/ayuda/valor por defecto en chino; la prioridad es `config.json` > entorno > valores de código, y guardar surte efecto inmediato (`restart_required=false`).

## Límite de seguridad

La lista de denegación y los parámetros de escape (exec / shell / attach) devuelven `E_UNSUPPORTED` antes de llegar a docker; la lista de contenedores protegidos evita paradas accidentales de producción; si docker.sock no es accesible, devuelve `E_NEED_ROOT` según ADR-0018 y apunta al canal de elevación de privilegios del núcleo, sin sugerir el grupo docker (equivalente a root); no Linux / sin CLI / daemon inaccesible siguen devolviendo `E_UNSUPPORTED`. El complemento corre como proceso aislado con socket 0600; eludir permisos o conectarse a otro socket es una vulnerabilidad.

## Compilación y verificación

La verificación diaria usa `make check` local (gofmt → go vet → go test -timeout 200s); antes de publicar, ejecuta `make dist` dos veces y confirma sha256 idéntico. GitHub Actions solo se dispara manualmente (`workflow_dispatch`), mientras Gitea Actions se dispara por tag de versión. `depth_level=basic`: las rutas principales funcionan; los casos límite y la inyección de fallos se posponen a v1.1, es una decisión de posicionamiento, no una degradación funcional.

## Enlaces relacionados

- [源码](https://github.com/kore-pl/kore-plugin-container)
- [kore 内核与协议](https://github.com/kore-pl/kore)
- [插件索引](https://github.com/kore-pl/kore-plugins)
