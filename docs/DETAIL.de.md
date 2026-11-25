# Plugin zur Container-Verwaltung（kore-plugin-container）

## Überblick

Dieses Plugin verwaltet vorhandene Container über die auf dem Host installierte `docker`-CLI: Auflistung, kompakte Inspection, Logs, Start/Stopp/Neustart und eine Statistikprobe. Es bringt kein Docker-SDK, keine gebündelte Runtime und keinen direkten Daemon-Socket mit (G-05 / G-17): jede externe Aktion ist ein `docker`-Subprozess mit festem argv (keine Shell). Fehlende CLI, nicht erreichbarer Daemon oder eine Nicht-Linux-Plattform liefern `E_UNSUPPORTED`, ohne stille Herabstufung oder erfundene Daten.

## Protokollfläche

Protokoll `kore.plugin.v1`, `lifecycle=lazy`, `depth_level=basic`, Fähigkeiten `system:read` + `system:admin`. Zwölf Geschäftsmethoden: `list` / `inspect` / `logs` / `stats` / `health` / `images` / `pull` / `rmi` / `update` / `start` / `stop` / `restart`, alle mit dem Domain-Präfix `container.`. `container.exec` / `shell` / `attach` werden bewusst nicht angeboten (ADR-0018) und geben `E_UNSUPPORTED` zurück, bevor docker erreicht wird. Die stdout des Handshakes ist für Rahmen reserviert; Protokolle gehen nach stderr.

## Kernfunktionen

Alle Container-Operationen laufen über `docker`-Subprozesse mit festem argv, nie über eine Shell; geschützte Container können nicht gestoppt oder gelöscht werden; `container.update` passt nur Ressourcenlimits an; `stats` ist eine einmalige Probe ohne Ticker oder Polling (G-19). Der Konfigurationsvertrag `container.config.get` / `container.config.set` definiert drei echte Optionen — `docker_bin`, `protected_containers`, `docker_timeout_ms` — alle mit chinesischem label/hilfe/default; die Reihenfolge ist `config.json` > Umgebung > Code-Defaults, und das Speichern wirkt sofort (`restart_required=false`).

## Sicherheitsgrenze

Die Ablehnungsliste und Escape-Parameter (exec / shell / attach) geben `E_UNSUPPORTED` zurück, bevor docker erreicht wird; die Liste geschützter Container verhindert versehentliche Produktions-Stopps; ist docker.sock nicht zugänglich, wird gemäß ADR-0018 `E_NEED_ROOT` zurückgegeben und auf den Kernel-Rechtekanal verwiesen, statt die docker-Gruppe (mit root-Rechten) zu empfehlen; Nicht-Linux / fehlende CLI / nicht erreichbarer Daemon liefern weiterhin `E_UNSUPPORTED`. Das Plugin läuft als isolierter Prozess mit 0600-Socket; Rechte zu umgehen oder einen fremden Socket zu verbinden ist eine Schwachstelle.

## Build und Verifizierung

Die tägliche Prüfung nutzt lokales `make check` (gofmt → go vet → go test -timeout 200s); vor dem Release `make dist` zweimal mit identischem sha256. GitHub Actions wird nur manuell ausgelöst (`workflow_dispatch`), Gitea Actions bei Versions-Tags. `depth_level=basic`: Kernpfade funktionieren, Grenzfälle und Fehlerinjektion sind auf v1.1 verschoben — eine Positionierung, kein funktionaler Rückschritt.

## Verwandte Links

- [源码](https://github.com/kore-pl/kore-plugin-container)
- [kore 内核与协议](https://github.com/kore-pl/kore)
- [插件索引](https://github.com/kore-pl/kore-plugins)
