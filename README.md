# SnowOps ANPR Backend Service

Backend-сервис для работы с ANPR-камерой Hikvision (модель DS-TCG406-E), который принимает события распознавания гос. номеров и обрабатывает их.

## Возможности

- Приём webhook-событий от ANPR-камеры
- Парсинг и сохранение событий с распознанными номерами
- Нормализация гос. номеров
- Проверка номеров по whitelist/blacklist
- Поиск событий и номеров через REST API

## Технологии

- Go 1.22+
- GORM + PostgreSQL
- Gin (HTTP router)
- Zerolog (логирование)
- Viper (конфигурация)

## Структура проекта

```
snowops-anpr-service/
├── cmd/
│   └── anpr-service/
│       └── main.go
├── internal/
│   ├── auth/          # JWT парсер
│   ├── config/        # Конфигурация
│   ├── db/            # Подключение к БД и миграции
│   ├── domain/        # Доменные модели
│   ├── http/          # HTTP handlers и router
│   ├── logger/        # Логгер
│   ├── model/         # Общие модели
│   ├── repository/    # Репозитории для работы с БД
│   ├── service/       # Бизнес-логика
│   └── utils/         # Утилиты
├── Dockerfile
├── docker-compose.yml
├── app.env
└── README.md
```

## Запуск

### Локально

1. Убедитесь, что PostgreSQL запущен
2. Скопируйте `.env.example` в `app.env` и настройте параметры
3. Запустите сервис:

```bash
go run cmd/anpr-service/main.go
```

### Docker Compose

```bash
docker compose up --build
```

Сервис будет доступен на `http://localhost:8082`

## API Endpoints

### Health Checks

- `GET /health/live` - проверка работоспособности
- `GET /health/ready` - проверка готовности (включая БД)

### ANPR Events

- `POST /api/v1/anpr/events` - приём события от камеры

Пример запроса:

```json
{
  "camera_id": "camera-001",
  "camera_model": "DS-TCG406-E",
  "plate": "123 ABC 02",
  "confidence": 0.95,
  "direction": "enter",
  "lane": 1,
  "event_time": "2025-01-21T12:34:56Z",
  "vehicle": {
    "color": "white",
    "type": "car"
  },
  "snapshot_url": "http://camera/snapshot.jpg"
}
```

Ответ:

```json
{
  "status": "ok",
  "event_id": 123,
  "plate_id": 45,
  "plate": "123ABC02",
  "hits": [
    {
      "list_id": 1,
      "list_name": "default_blacklist",
      "list_type": "BLACKLIST"
    }
  ]
}
```

### Plates

- `GET /api/v1/plates?plate=123ABC02` - поиск номеров

Ответ:

```json
{
  "data": [
    {
      "id": 45,
      "number": "123 ABC 02",
      "normalized": "123ABC02",
      "last_event_time": "2025-01-21T12:34:56Z"
    }
  ]
}
```

### Events

- `GET /api/v1/events?plate=123ABC02&from=2025-01-01T00:00:00Z&to=2025-01-31T23:59:59Z&limit=50&offset=0` - поиск событий

## База данных

Сервис создаёт следующие таблицы:

- `plates` - номера (исходный и нормализованный)
- `vehicles` - информация о ТС
- `anpr_events` - события распознавания
- `lists` - списки (whitelist/blacklist)
- `list_items` - элементы списков

Миграции выполняются автоматически при старте сервиса.

## Конфигурация

Все параметры настраиваются через переменные окружения (см. `.env.example`):

- `APP_ENV` - окружение (development/production)
- `HTTP_HOST` - хост для HTTP сервера
- `HTTP_PORT` - порт для HTTP сервера
- `DB_DSN` - строка подключения к PostgreSQL
- `JWT_ACCESS_SECRET` - секрет для JWT токенов
- `CAMERA_RTSP_URL` - RTSP URL камеры
- `CAMERA_HTTP_HOST` - HTTP хост камеры
- `CAMERA_MODEL` - модель камеры
- `HIK_CONNECT_DOMAIN` - домен HikConnect
- `ENABLE_SNOW_VOLUME_ANALYSIS` - включить анализ объёма снега

