# Plugin de gestion des conteneurs（kore-plugin-container）

## Aperçu

Ce plugin gère les conteneurs existants via la CLI `docker` installée sur l’hôte : liste, inspection concise, journaux, démarrage/arrêt/redémarrage et un échantillonnage statistique. Aucun SDK Docker, aucun runtime intégré et aucun accès direct au socket du daemon (G-05 / G-17) : chaque action externe est un sous-processus `docker` à argv fixe (sans shell). Une CLI absente, un daemon inaccessible ou une plateforme non Linux renvoie `E_UNSUPPORTED`, sans repli silencieux ni données forgées.

## Surface du protocole

Protocole `kore.plugin.v1`, `lifecycle=lazy`, `depth_level=basic`, capacités `system:read` + `system:admin`. Douze méthodes métier : `list` / `inspect` / `logs` / `stats` / `health` / `images` / `pull` / `rmi` / `update` / `start` / `stop` / `restart`, toutes avec le préfixe de domaine `container.`. `container.exec` / `shell` / `attach` sont explicitement absents (ADR-0018) et renvoient `E_UNSUPPORTED` avant tout accès à docker. La sortie standard du handshake est réservée aux trames ; les journaux vont sur stderr.

## Fonctionnalités clés

Toutes les opérations passent par des sous-processus `docker` à argv fixe, jamais un shell ; les conteneurs protégés ne peuvent être arrêtés ni supprimés ; `container.update` ne règle que les limites de ressources ; `stats` est un échantillon unique, sans ticker ni sondage (G-19). Le contrat de configuration `container.config.get` / `container.config.set` définit trois options réelles : `docker_bin`, `protected_containers`, `docker_timeout_ms`, avec libellé/aide/valeur par défaut en chinois ; la priorité est `config.json` > environnement > valeurs du code, et l’enregistrement prend effet immédiatement (`restart_required=false`).

## Périmètre de sécurité

La liste de refus et les paramètres d’évasion (exec / shell / attach) renvoient `E_UNSUPPORTED` avant d’atteindre docker ; la liste des conteneurs protégés évite les arrêts accidentels en production ; si docker.sock est inaccessible, `E_NEED_ROOT` est renvoyé selon l’ADR-0018 avec un renvoi vers le canal d’élévation de privilèges du noyau, sans suggérer le groupe docker (équivalent root) ; non-Linux / CLI absente / daemon inaccessible renvoient toujours `E_UNSUPPORTED`. Le plugin s’exécute en processus isolé avec un socket 0600 ; contourner les permissions ou se connecter à un autre socket est une vulnérabilité.

## Compilation et vérification

La vérification quotidienne utilise `make check` en local (gofmt → go vet → go test -timeout 200s) ; avant publication, `make dist` deux fois avec sha256 identique. GitHub Actions est déclenché manuellement (`workflow_dispatch`), tandis que Gitea Actions se déclenche sur les tags de version. `depth_level=basic` : les chemins principaux fonctionnent, les cas limites et l’injection de fautes sont reportés à v1.1 — un choix de positionnement, pas une régression fonctionnelle.

## Liens connexes

- [源码](https://github.com/kore-pl/kore-plugin-container)
- [kore 内核与协议](https://github.com/kore-pl/kore)
- [插件索引](https://github.com/kore-pl/kore-plugins)
