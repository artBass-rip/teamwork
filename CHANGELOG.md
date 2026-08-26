# Changelog

Все заметные изменения TeamWork документируются в этом файле. Формат основан на [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), версии следуют [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Planned

- Slack multi-workspace connector и Socket Mode;
- OneNote Personal connector через Microsoft Graph delegated OAuth;
- связи Slack ↔ Jira ↔ OneNote и workflow-модули;
- единый индекс поиска и интерфейсы LLM/RAG;
- локальный пользовательский CLI для сценариев интеграции.

## [2.0.0-alpha.1] — 2026-08-26

### Added

- Go microkernel, localhost HTTP shell и portable subprocess supervisor;
- Unix Domain Socket JSON-RPC 2.0 transport и protocol v1;
- manifest discovery, capability registry, durable event bus и SQLite/WAL storage;
- системный Secret Broker для macOS Keychain и Linux Secret Service;
- журнал `DEBUG`/`INFO`/`WARN`/`ERROR` с фильтрацией и retention 48 часов;
- генерация macOS LaunchAgent и Ubuntu `systemd --user` unit;
- Go SDK и reference-модули Activity и Echo;
- Project View с общим списком задач и вкладками Jira-проектов;
- Jira Cloud browser OAuth через Atlassian Rovo MCP, DCR и PKCE;
- multi-account Jira, мультивыбор проектов и полная постраничная синхронизация;
- Jira-ссылки, автор, исполнитель, статус и состояние спринта;
- группировка спринтов по состоянию и номеру;
- live-фильтры с мультивыбором и поиск по задачам;
- локальные комментарии и локальные метки с сохранением после Jira sync;
- каталог меток, создание, выбор, фильтрация, группировка, снятие и глобальное удаление;
- bulk-назначение локальных меток через capability;
- CSV-экспорт текущего отфильтрованного списка;
- сортировка Jira issues по убыванию числовой части ключа;
- portable release-сборки для macOS/Linux arm64 и amd64.

### Changed

- прежнее Node.js/Docker-приложение полностью заменено microkernel-архитектурой;
- прикладной функционал перенесён из монолитного core в автономные модули;
- Jira authentication заменена на browser OAuth без хранения паролей, cookies и API token в базе;
- Jira labels и comments исключены из импорта в пользу локальных данных TeamWork;
- web UI переработан в единое локальное приложение Project View.

### Removed

- обязательные Node.js, npm, Python, Docker и Compose runtime-зависимости;
- legacy MCP proxy и старый монолитный web/server code;
- хранение интеграционных секретов в файлах приложения или SQLite;
- необходимость публичного callback endpoint для Jira-авторизации.

### Security

- IPC socket с правами `0600` и одноразовые subprocess tokens;
- OAuth/provider tokens в системном хранилище секретов;
- защита CSV от spreadsheet formula injection;
- HTTP server по умолчанию слушает только `127.0.0.1`.

[Unreleased]: https://github.com/artBass-rip/teamwork/compare/v2.0.0-alpha.1...HEAD
[2.0.0-alpha.1]: https://github.com/artBass-rip/teamwork/releases/tag/v2.0.0-alpha.1
