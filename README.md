# GoArmMon

Сервис мониторинга хостов на Go с уведомлениями и управлением через Telegram-бота.

## Возможности

- **Проверки**: ICMP ping, HTTP, метрики через [Glances](https://github.com/nicolargo/glances) API
- **Алерты**: пороги warning/critical/recovery для CPU, RAM, swap, дисков, RTT и времени ответа HTTP; параметр `for` — минимальная длительность превышения порога перед отправкой оповещения (короткие пики не алертят); `fail_threshold` у ping — число подряд неудачных проверок до offline-алерта; промежуточный статус `PARTIAL (порог/текущее)` в боте без push-уведомлений (включить: `/notify_partial on`)
- **Telegram**: push-уведомления о событиях и команды для просмотра статуса; роли root / user / limited_admin
- **Горячая перезагрузка** конфигурации без перезапуска
- **Сборка** под Linux amd64 и armv7 (без CGO)

## Быстрый старт

```bash
cp config.yaml.example config.yaml
# укажите telegram.token и telegram.allowed_users (первый ID — root)

make build
./bin/monitor -config config.yaml
```

## Конфигурация

Настройки задаются в YAML-файле (`config.yaml` по умолчанию). Пример — в `config.yaml.example`.

Основные секции:

| Секция | Описание |
|--------|----------|
| `telegram` | токен бота, `allowed_users` (первый ID всегда root) и `db_path` — каталог SQLite с ролями и хостами (пусто = `{каталог конфига}/db`) |
| `logging` | уровень логов, путь к файлу (пусто — stdout) и `log_results` — писать итог каждой проверки (ping/http/glances) в лог |
| `hosts` | необязательный seed при первом запуске: хосты импортируются в SQLite и удаляются из YAML; источник правды — `{db_path}/acl.sqlite` |

Для каждого хоста можно задать группу, описание и флаг `skip_on_ping_failure` — пропускать остальные проверки, если ping недоступен. Добавление и редактирование хостов — в боте: Hosts → Add host / Manage. Если хосты указаны в YAML, при старте или hot-reload они переносятся в SQLite (в лог пишется `imported host from config` или `skipped host already in database`) и удаляются из конфига.

Секция `messages` (необязательная) задаёт текст Telegram-уведомлений для событий `offline`, `online`, `warning`, `critical` и `recovery`. Пустое поле оставляет стандартное техническое сообщение (`host/check: OLD -> NEW`). Заголовок события и время остаются. Настраивается в YAML (до импорта) и в карточке хоста в боте (секция Messages).

Роли пользователей (кроме встроенного root) и список хостов мониторинга хранятся в SQLite `{db_path}/acl.sqlite`. Добавление пользователей, смена роли и список хостов для просмотра/алертов — в боте: Settings → Users.

| Роль | Права |
|------|--------|
| `root` | полный доступ (первый ID в `allowed_users` всегда root и не снимается) |
| `user` | просмотр и алерты по всем хостам или по списку, который задаёт root |
Каждый входящий запрос в бот пишется в лог с `user_id`, `role` и `access` (какие хосты доступны).

## Команды Telegram-бота

| Команда | Описание |
|---------|----------|
| `/status` | сводка по всем хостам |
| `/host <name>` | детали по хосту |
| `/list` | список хостов |
| `/alerts` | активные алерты |
| `/ping`, `/http`, `/glances` | результаты соответствующих проверок |
| `/uptime` | uptime из Glances |
| `/help` | справка |

## Сборка

```bash
make build          # локальная сборка → bin/monitor
make amd64          # bin/monitor-linux-amd64
make arm            # bin/monitor-linux-armv7
make all            # обе платформы
```

## Требования

- Go 1.25+
- Telegram Bot Token
- Для проверки `glances` — запущенный Glances с HTTP API на целевом хосте
