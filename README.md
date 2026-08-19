# GoArmMon

Сервис мониторинга хостов на Go с уведомлениями и управлением через Telegram-бота.

## Возможности

- **Проверки**: ICMP ping, HTTP, метрики через [Glances](https://github.com/nicolargo/glances) API
- **Алерты**: пороги warning/critical/recovery для CPU, RAM, swap, дисков, RTT и времени ответа HTTP; параметр `for` — минимальная длительность превышения порога перед отправкой оповещения (короткие пики не алертят); `fail_threshold` у ping — число подряд неудачных проверок до offline-алерта; промежуточный статус `PARTIAL (порог/текущее)` в боте без push-уведомлений (включить: `/notify_partial on`)
- **Telegram**: push-уведомления о событиях и команды для просмотра статуса
- **Горячая перезагрузка** конфигурации без перезапуска
- **Сборка** под Linux amd64 и armv7 (без CGO)

## Быстрый старт

```bash
cp config.yaml.example config.yaml
# укажите telegram.token и telegram.allowed_users

make build
./bin/monitor -config config.yaml
```

## Конфигурация

Настройки задаются в YAML-файле (`config.yaml` по умолчанию). Пример — в `config.yaml.example`.

Основные секции:

| Секция | Описание |
|--------|----------|
| `telegram` | токен бота и список разрешённых user ID |
| `logging` | уровень логов и путь к файлу (пусто — stdout) |
| `hosts` | список хостов, их проверки и пороги алертов |

Для каждого хоста можно задать группу, описание и флаг `skip_on_ping_failure` — пропускать остальные проверки, если ping недоступен.

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

- Go 1.23+
- Telegram Bot Token
- Для проверки `glances` — запущенный Glances с HTTP API на целевом хосте
