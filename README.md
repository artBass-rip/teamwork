# TeamWork Integration Hub

TeamWork — локальный Integration Hub для персональной работы с Jira и последующего подключения Slack, OneNote и других сервисов. Продукт построен вокруг лёгкого Go microkernel: ядро отвечает только за запуск, безопасность и взаимодействие модулей, а вся прикладная функциональность поставляется отдельными portable executable subprocess.

Текущая версия: `2.0.0-alpha.2`.

## Что уже работает

- нативный запуск на macOS и Ubuntu без Python, Node.js, Docker и виртуальных окружений;
- отдельный локальный subprocess для каждого модуля;
- Unix Domain Socket JSON-RPC 2.0 с правами `0600` и одноразовым IPC-токеном процесса;
- обнаружение portable-модулей по manifest и автоматический restart после сбоя;
- capability router и durable event bus с состоянием доставки в SQLite;
- SQLite/WAL для состояния ядра, событий и логов;
- уровни логирования `DEBUG`, `INFO`, `WARN`, `ERROR`, фильтрация в UI и retention 48 часов;
- системное хранилище секретов: macOS Keychain или Linux Secret Service;
- localhost web UI на `http://127.0.0.1:8090`;
- подключение нескольких Jira Cloud аккаунтов через browser-based OAuth;
- выбор нескольких Jira-проектов и полная постраничная синхронизация задач;
- общий список задач и отдельные вкладки проектов;
- группировка по спринтам: незавершённые по убыванию номера, затем backlog, затем завершённые;
- фильтры по статусу, исполнителю, автору, ключевым словам и локальным меткам;
- локальные комментарии и метки, сохраняемые между Jira-синхронизациями;
- создание, выбор, снятие и глобальное удаление локальных меток;
- включаемая группировка задач по локальным меткам;
- ссылки на Jira и CSV-экспорт текущего отфильтрованного списка;
- установка как пользовательского LaunchAgent или `systemd --user` service;
- Slack multi-workspace connector через Socket Mode;
- OneNote Personal connector с delegated OAuth;
- сохранение отдельного Slack-сообщения или всего thread на существующую либо новую страницу `Operation Notes`.

Единый полнотекстовый/RAG-поиск и дополнительные межсервисные workflow входят в целевую архитектуру, но ещё не реализованы в текущем alpha-срезе.

## Архитектура

```text
Browser / local API clients
            │ HTTP on 127.0.0.1
            ▼
┌─────────────────────────────────────────────┐
│              TeamWork Go Core               │
│ HTTP shell · supervisor · capability router │
│ durable events · SQLite · secret broker     │
└──────────────────────┬──────────────────────┘
                       │ Unix socket / NDJSON
          ┌────────────┼──────────────┐
          ▼            ▼              ▼
   Project View      Jira         Activity / Echo
    subprocess     subprocess       subprocesses
```

Основные правила:

1. Модуль является автономным исполняемым файлом и запускается ядром как отдельный subprocess.
2. Модули не импортируют код и не вызывают процессы друг друга напрямую.
3. Вызовы проходят через именованные capabilities ядра.
4. События доставляются как минимум один раз: результат каждой попытки фиксируется в SQLite, а pending/failed delivery повторяется после регистрации subscriber. Обработчики должны быть идемпотентными.
5. Секреты не хранятся в SQLite, manifest, исходном коде или state-файлах модулей.
6. Новый модуль подключается размещением каталога с `module.json` и бинарником нужной платформы.

Контракты: [протокол модулей](specs/protocol-v1.md) и [JSON Schema manifest](specs/module-manifest.schema.json).

## Состав репозитория

```text
core/                    Go microkernel и localhost HTTP shell
modules/
  activity-go/           журнал демонстрационных событий
  echo-go/               минимальный пример capability-модуля
  jira-go/               Jira Cloud OAuth, проекты и синхронизация
  project-view-go/       локальная модель задач и web UI
  slack-go/              Slack multi-workspace и Socket Mode
  onenote-go/            Microsoft Graph delegated OAuth
  slack-onenote-go/      workflow message/thread → OneNote
sdk/go/                  Go runtime SDK для portable-модулей
specs/                   protocol v1 и schema manifest
scripts/build.sh          сборка текущей платформы
scripts/release.sh        кроссплатформенные release-пакеты
start.sh                  локальный запуск
```

## Требования

Для разработки нужен Go `1.26` или новее. Для запуска готового release-пакета Go и другие runtime-окружения не нужны.

Поддерживаемые цели:

- macOS arm64 и amd64;
- Linux arm64 и amd64.

На Linux для системного хранилища секретов нужен `secret-tool` и доступный Secret Service, например GNOME Keyring.

## Быстрый старт

```bash
./scripts/build.sh
./start.sh
```

Откройте [http://127.0.0.1:8090](http://127.0.0.1:8090).

Если порт занят, остановите уже запущенный экземпляр или задайте другой адрес:

```bash
TEAMWORK_ADDRESS=127.0.0.1:8091 ./start.sh
```

## Команды ядра

```bash
./core/bin/teamwork serve            # запустить приложение
./core/bin/teamwork version          # показать версию ядра
./core/bin/teamwork modules          # показать обнаруженные модули
./core/bin/teamwork paths            # показать системные пути
./core/bin/teamwork service-install  # создать definition системного сервиса
```

`service-install` создаёт файл сервиса, но не активирует его автоматически.

macOS:

```bash
launchctl load ~/Library/LaunchAgents/com.teamwork.hub.plist
```

Ubuntu:

```bash
systemctl --user daemon-reload
systemctl --user enable --now teamwork.service
```

## Подключение Jira

1. Запустите TeamWork и откройте раздел «Интеграции».
2. Нажмите «Войти в Jira».
3. Завершите авторизацию на официальной странице Atlassian.
4. Выберите один или несколько доступных проектов.
5. Запустите синхронизацию.

TeamWork использует официальный Atlassian Rovo MCP endpoint и OAuth 2.1 discovery, Dynamic Client Registration и PKCE. Пользователю не нужны `Client ID`, `Client Secret`, API token, пароль или cookies браузера. Callback принимается только локально:

```text
http://127.0.0.1:8976/oauth/jira/callback
```

Организация Atlassian может потребовать разрешить Rovo MCP и localhost redirect в Admin Hub.

После синхронизации Jira connector передаёт Project View данные задач, автора, исполнителя, статус, ссылку и состояние спринта. Jira labels и Jira comments намеренно не импортируются: метки и комментарии TeamWork являются локальными пользовательскими данными.

## Slack → OneNote

### Настройка Slack App

Создайте Slack App через **Create New App → From a manifest**, выберите workspace и вставьте следующий YAML:

```yaml
display_information:
  name: TeamWork Integration Hub
  description: Save Slack messages and threads to OneNote
  background_color: "#4f46e5"

features:
  bot_user:
    display_name: TeamWork
    always_online: false
  shortcuts:
    - name: Save to OneNote
      type: message
      callback_id: teamwork_save_onenote
      description: Save this message or its thread to OneNote

oauth_config:
  scopes:
    bot:
      - channels:history
      - chat:write
      - commands
      - groups:history
      - im:history
      - mpim:history
      - users:read

settings:
  interactivity:
    is_enabled: true
  org_deploy_enabled: false
  socket_mode_enabled: true
  token_rotation_enabled: false
```

После создания приложения:

1. Откройте **Basic Information → App-Level Tokens** и создайте token со scope `connections:write`. Скопируйте полученный `xapp-…` — Slack показывает его только один раз.
2. Откройте **OAuth & Permissions**, установите приложение в workspace и скопируйте **Bot User OAuth Token** `xoxb-…`.
3. Пригласите бота в закрытые каналы, сообщения которых требуется сохранять. Наличие `groups:history` само по себе не предоставляет доступ к каналам, участником которых бот не является.
4. В TeamWork откройте **Интеграции → Slack**, укажите название workspace, `xoxb-…` и `xapp-…`, затем нажмите **Подключить Slack workspace**.

Scope `commands` обязателен для регистрации message shortcut, даже если приложение не использует slash-команды. Публичный Request URL, Events API и внешний сервер не требуются. Если приложение будет работать только в публичных каналах, необязательные scopes `groups:history`, `im:history` и `mpim:history` можно удалить из манифеста. После изменения scopes переустановите приложение в workspace.

Можно подключить несколько workspaces. Токены каждого workspace сохраняются только в системном Secret Broker.

### Настройка OneNote

1. Создайте public-client application в Microsoft Entra. Для личного OneNote выберите supported account type **Accounts in any organizational directory and personal Microsoft accounts**.
2. Добавьте delegated permissions `User.Read`, `Notes.ReadWrite` и `offline_access`.
3. Добавьте redirect URI типа **Mobile and desktop applications**: `http://localhost` — без пути и без номера порта.
4. Разрешите public client flows.
5. Введите application/client ID в TeamWork и выполните вход в OneNote.

Client secret не используется. OAuth callback запускается на свободном локальном loopback-порту; Microsoft сопоставляет его с зарегистрированным `http://localhost`. Connector находит секцию `Operation Notes`, а при её отсутствии создаёт её в первом доступном notebook.

### Сохранение

В Slack выберите сообщение → More actions → Save to OneNote. Modal позволяет выбрать:

- только выбранное сообщение или весь thread;
- существующую страницу `Operation Notes` или создание новой страницы;
- OneNote account;
- название новой страницы.

Thread загружается полностью с пагинацией. Операция выполняется асинхронно, результат отправляется пользователю ephemeral-сообщением, а локальная связь Slack ↔ OneNote сохраняется workflow-модулем.

## Работа с задачами

### Списки и фильтры

Вкладка «Все задачи» объединяет выбранные Jira-проекты. Каждый проект также имеет собственную вкладку. Доступны live-фильтры с мультивыбором по статусу, исполнителю, автору и локальным меткам, а также поиск по ключевым словам.

Задачи сортируются по убыванию числовой части Jira-ключа: `DO-100`, `DO-20`, `DO-9`.

### Спринты

Подвкладка «По спринтам» группирует задачи в порядке:

1. незавершённые спринты (`active`, `future`) по убыванию номера;
2. backlog;
3. завершённые спринты (`closed`, `completed`) по убыванию номера.

### Локальные метки

- метки создаются и хранятся только в TeamWork;
- существующие метки предлагаются во время ввода;
- новый текст создаёт новую метку;
- крестик в карточке снимает метку с одной задачи;
- крестик в каталоге фильтра удаляет метку со всех задач после подтверждения;
- клик по метке включает соответствующий фильтр;
- группировку по меткам можно включать и отключать.

Локальные метки и комментарии сохраняются при последующих Jira-синхронизациях.

### CSV-экспорт

Кнопка «Экспорт CSV» выгружает текущий список с учётом выбранного проекта, поиска и фильтров. Файл содержит проект, ключ и название задачи, статус, исполнителя, автора, спринт, локальные метки и комментарии, а также ссылку Jira.

CSV формируется локально в браузере, кодируется в UTF-8 с BOM и экранирует потенциальные spreadsheet formulas.

Кнопка «Импорт Jira» предназначена для ручного импорта локального JSON snapshot. Обычная Jira-синхронизация выполняется кнопкой «Обновить» или из раздела интеграции.

## Локальные данные

Пути можно посмотреть командой:

```bash
./core/bin/teamwork paths
```

По умолчанию на macOS данные находятся в:

```text
~/Library/Application Support/TeamWork/
~/Library/Logs/TeamWork/
```

На Ubuntu используются XDG-каталоги:

```text
~/.config/teamwork/
~/.local/share/teamwork/
~/.local/state/teamwork/
```

Для изолированного тестового запуска можно задать `TEAMWORK_HOME`. Каталог модулей переопределяется через `TEAMWORK_MODULES_DIR`, адрес HTTP — через `TEAMWORK_ADDRESS`.

## API и capabilities

```bash
curl -sS http://127.0.0.1:8090/api/health

curl -sS -X POST \
  -H 'content-type: application/json' \
  -d '{"payload":{"message":"hello"}}' \
  http://127.0.0.1:8090/api/capabilities/example.echo
```

HTTP API доступен только на loopback по умолчанию. Не публикуйте его во внешнюю сеть без отдельной аутентификации и reverse proxy.

## Создание модуля

```text
modules/example/
  module.json
  bin/
    darwin-arm64/example
    darwin-amd64/example
    linux-arm64/example
    linux-amd64/example
```

Manifest объявляет идентификатор, версию протокола, исполняемые файлы, предоставляемые capabilities и event subscriptions. Ядро передаёт процессу:

- `TEAMWORK_CORE_SOCKET`;
- `TEAMWORK_MODULE_ID`;
- `TEAMWORK_MODULE_TOKEN`;
- `TEAMWORK_MODULE_DATA`.

Модуль может быть написан на любом языке, если он выпускается как автономный executable и реализует protocol v1. Go SDK в `sdk/go` является референсной реализацией, а не обязательной зависимостью.

Go-модуль должен дождаться `Runtime.Ready()` перед фоновыми RPC-вызовами. SDK открывает readiness только после подтверждённой регистрации и немедленно завершает ожидающие вызовы при потере связи с ядром.

## Разработка и проверки

```bash
(cd core && go test -race ./... && go vet ./...)
(cd sdk/go && go test -race ./... && go vet ./...)
(cd modules/echo-go && go test -race ./... && go vet ./...)
(cd modules/activity-go && go test -race ./... && go vet ./...)
(cd modules/project-view-go && go test -race ./... && go vet ./...)
(cd modules/jira-go && go test -race ./... && go vet ./...)
(cd modules/slack-go && go test -race ./... && go vet ./...)
(cd modules/onenote-go && go test -race ./... && go vet ./...)
(cd modules/slack-onenote-go && go test -race ./... && go vet ./...)
```

```bash
./scripts/build.sh    # текущая платформа
./scripts/release.sh  # macOS/Linux arm64/amd64 в dist/
```

Артефакты `core/bin/`, `modules/*/bin/` и `dist/` не коммитятся.

## Безопасность

О правилах хранения секретов, модели доверия localhost и сообщении об уязвимостях см. [SECURITY.md](SECURITY.md).

## История изменений

См. [CHANGELOG.md](CHANGELOG.md).

## Лицензия

[MIT](LICENSE) © 2026 artBass-rip
