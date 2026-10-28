# Ticket Booking System — Сервис бронирования билетов с защитой от овербукинга

Backend-сервис на Go для поиска мероприятий, временного удержания мест (Hold на 10 минут) и идемпотентного оформления заказов при высокой конкуренции запросов.

## Ключевые архитектурные механизмы

1. **Тройная защита от двойного бронирования (Overbooking Protection)**
   * **Распределённая блокировка (Redis SET NX PX):** перед изменением статуса места захватывается краткоживущий лок по ключу `seat_lock:{event_id}:{seat_number}` с уникальным токеном владельца (`internal/lock/distributed_lock.go`).
   * **Оптимистичная блокировка (CAS по `Seat.Version`):** каждое изменение состояния места (`AVAILABLE -> HELD -> SOLD`) атомарно проверяет и инкрементирует поле `version` (`version = version + 1`), предотвращая гонки при подтверждении оплаты.
   * **Транзакционная изоляция:** проверка доступности места и запись резерва выполняются атомарно.
2. **Механизм временного удержания места (Hold TTL = 10 минут)**
   * При вызове `POST /api/v1/bookings/hold` место переводится в статус `HELD` с таймером `hold_expires_at = NOW() + 10m`.
   * Фоновый воркер (`StartHoldReaper`) периодически сканирует истёкшие резервы и возвращает места в статус `AVAILABLE`, а также выполняется ленивая проверка при чтении и новой попытке бронирования.
3. **Идемпотентное API оплаты и подтверждения (`Idempotency-Key`)**
   * Эндпоинт `POST /api/v1/bookings/confirm` требует заголовок `Idempotency-Key`.
   * При сетевых сбоях и повторной отправке запроса клиентом сервис возвращает сохранённый заказ с заголовком `X-Idempotent-Replay: true` без повторного списания средств.
4. **Ролевая авторизация (RBAC) и JWT (Access + Refresh токены)**
   * Реализован выпуск и валидация `HS256` JWT-токенов (`access` и `refresh`) и middleware разграничения прав (`USER` и `ADMIN`).

## Структура проекта

```text
ticket-booking/
├── cmd/booking/main.go                 # Точка входа, запуск фонового воркера и HTTP-сервера
├── docs/openapi.yaml                   # Документация REST API в формате OpenAPI 3.0 / Swagger
├── internal/
│   ├── api/http_handler.go             # HTTP-маршруты, JWT и RBAC middleware
│   ├── auth/jwt.go                     # Генерация и проверка Access/Refresh JWT токенов
│   ├── domain/models.go                # Модели Event, Seat (с Version), HoldReservation, Order
│   ├── lock/distributed_lock.go        # Распределённая блокировка по модели Redis SET NX PX
│   └── service/
│       ├── booking_service.go          # Бизнес-логика резервирования, TTL-очистки и оплаты
│       └── booking_service_test.go     # Интеграционные тесты конкурентного бронирования (-race)
├── migrations/001_init.sql             # SQL-схема PostgreSQL (users, events, seats, orders)
├── Dockerfile
├── docker-compose.yml
└── Makefile
```

## Быстрый старт

### Запуск через Docker Compose
```bash
docker compose up --build -d
```
Сервис будет доступен на `http://localhost:8081`, а спецификация OpenAPI — по адресу `http://localhost:8081/swagger/openapi.yaml`.

### Запуск тестов конкурентного бронирования с Race Detector
```bash
make race
make bench
```
Тест `TestConcurrentHold_ZeroOverbooking` запускает 200 горутин одновременно в одну микросекунду на место `A-1` и проверяет, что ровно 1 запрос получает `HoldReservation`, а остальные 199 получают `409 Conflict`.
