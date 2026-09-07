# Семейный Telegram-бот расписания

Приватный read-only Telegram-бот на Go. Загружает четыре Google Calendar по секретным iCal URL, показывает расписание на сегодня, завтра и текущую неделю, поддерживает повторяющиеся и многодневные события. Доступ хранится в SQLite и управляется администратором из Telegram.

## Возможности

- расписание Ани, Лёши, Саши и Насти;
- «Сегодня всех» и «Завтра всех»;
- RRULE, RDATE, EXDATE и RECURRENCE-ID overrides;
- DATE/all-day и события, пересекающие границу дня;
- кеш каждого календаря и fallback на последние успешные данные;
- роли `admin`/`user`, soft-deactivation и защита последнего администратора;
- добавление пользователя по числовому Telegram ID;
- одноразовые приглашения с TTL и SHA-256 hash в БД;
- структурированные логи без токенов и URL.

## 1. Создание бота

Откройте `@BotFather`, выполните `/newbot` и сохраните полученный токен в `.env`. Если токен был опубликован, выполните `/revoke` и используйте новый. Никогда не добавляйте токен в Git.

## 2. Числовой Telegram User ID

Для `ADMIN_TELEGRAM_USER_ID` нужен положительный числовой ID, а не username. Его можно получить через `@userinfobot` или из поля `message.from.id` метода Telegram `getUpdates` после сообщения собственному боту.

## 3. Секретные iCal URL

Для каждого календаря: Google Calendar → Настройки → нужный календарь → Интеграция календаря → **Секретный адрес в формате iCal**. Google Calendar API, OAuth и service accounts не используются.

Секретный iCal URL предоставляет read-only доступ ко всему календарю. Не публикуйте его, не отправляйте в логи и отзовите/пересоздайте адрес при утечке.

## 4. Конфигурация

Скопируйте `.env.example` в `.env` и заполните локально:

```env
TELEGRAM_BOT_TOKEN=
ADMIN_TELEGRAM_USER_ID=
CALENDAR_ANYA_ICAL_URL=
CALENDAR_LESHA_ICAL_URL=
CALENDAR_SASHA_ICAL_URL=
CALENDAR_NASTYA_ICAL_URL=
TIMEZONE=Europe/Moscow
DATABASE_PATH=/app/data/bot.db
ICAL_CACHE_TTL=2m
ICAL_HTTP_TIMEOUT=10s
INVITE_TTL=24h
```

Все четыре URL обязаны быть HTTPS. `TIMEZONE` должен быть валидным IANA timezone. `.env` исключён из Git и Docker build context.

## 5. Локальная разработка и тесты

Требуется Go 1.22+.

```bash
go mod download
go test ./...
go test -race ./...
```

Тестам не нужны Telegram, Google Calendar или интернет. Для локального запуска задайте переменные окружения и выполните:

```bash
go run ./cmd/bot
```

## 6. Docker

```bash
mkdir -p data
docker compose build
docker compose up -d
docker compose logs -f --tail=100
```

Образ собирается multi-stage и запускается как непривилегированный пользователь. SQLite сохраняется через `./data:/app/data`. `.env` не копируется в образ.

## 7. VPS deployment

Целевой каталог: `/opt/family-calendar-bot`.

```bash
git clone git@github.com:eusatenko/calendar_telegramm_bot.git /opt/family-calendar-bot
cd /opt/family-calendar-bot
install -d -m 700 data
# создать .env вручную; существующий .env не перезаписывать
docker compose build
docker compose up -d
docker compose ps
docker compose logs --tail=100
```

Обновление:

```bash
cd /opt/family-calendar-bot
git pull --ff-only
docker compose build
docker compose up -d
```

Webhook не нужен: бот использует long polling, поэтому входящий порт для приложения открывать не требуется.

## 8. Управление доступом

При старте `ADMIN_TELEGRAM_USER_ID` атомарно создаётся или восстанавливается как активный администратор. Неавторизованный пользователь получает только «Доступ не предоставлен».

Администратор открывает **Управление доступом** и может:

- просмотреть пользователей;
- добавить или реактивировать пользователя по числовому ID;
- деактивировать пользователя;
- создать одноразовую deep link-ссылку.

Ожидание ID действует пять минут и отменяется `/cancel` или кнопкой отмены. Все admin-операции повторно проверяют роль на сервере. Последний активный admin не деактивируется.

Приглашение создаётся из 32 криптографически случайных байт. В SQLite хранится только SHA-256 hash. Применение, активация пользователя и отметка `used_at` выполняются в одной транзакции, поэтому параллельное повторное использование не проходит.

## 9. Кеш

Каждый календарь имеет отдельный parsed in-memory кеш. По умолчанию TTL — две минуты. Параллельные запросы объединяются в один refresh. При ошибке refresh используются последние успешные данные с предупреждением; при отсутствии кеша показывается короткая ошибка.

## 10. Логи

```bash
docker compose logs --tail=200 bot
```

Логи содержат логическое имя календаря, операцию, длительность и ошибку. Токен Telegram, полный iCal URL и plaintext invite token не логируются.

## 11. Backup и restore SQLite

Остановите контейнер для согласованной файловой копии:

```bash
docker compose stop bot
cp -a data/bot.db "data/bot.db.backup-$(date +%Y%m%d-%H%M%S)"
docker compose start bot
```

Для восстановления остановите контейнер, сохраните текущую БД отдельно, скопируйте backup в `data/bot.db`, выставьте владельца каталога и запустите контейнер.

## 12. Диагностика

- `configuration error`: проверьте обязательные переменные, числовой admin ID, IANA timezone и HTTPS URL.
- `HTTP 403/404`: секретный адрес отозван или скопирован не полностью.
- timeout: проверьте DNS/HTTPS-доступ VPS и `ICAL_HTTP_TIMEOUT`.
- malformed iCal / неизвестный TZID: пересоздайте secret address и проверьте исходный календарь. Поддерживаются стандартные IANA TZID; нестандартный TZID из произвольного VTIMEZONE отклоняется безопасно.
- `database open failed`: проверьте права `/app/data` и свободное место.
- бот не отвечает: `docker compose ps`, затем `docker compose logs --tail=200 bot`.

## 13. Ротация секретов

- Telegram: `/revoke` в BotFather, заменить только `TELEGRAM_BOT_TOKEN` в production `.env`, перезапустить контейнер.
- Calendar: сбросить secret iCal address в Google Calendar, заменить соответствующий `CALENDAR_*_ICAL_URL`, перезапустить контейнер.

После изменения:

```bash
docker compose up -d --force-recreate bot
```
