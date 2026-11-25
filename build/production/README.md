# Сборка и развёртывание в промышленной среде

Комплект для промышленной среды создаёт неизменяемые образы backend:

- `online-market-api` содержит gRPC API и CLI создания администратора;
- `online-market-migrations` содержит зафиксированную версию go-migrate и
  SQL-миграции из репозитория.

HTTP/JSON gateway собирается отдельно в репозитории `online_market_gateway` и
подключается через `GATEWAY_IMAGE`.

PostgreSQL и Redis являются внешними зависимостями. Следует использовать
управляемые сервисы или отдельно обслуживаемый кластер с резервным копированием,
мониторингом и TLS. Этот Compose-файл не запускает хранилища данных.

## Сборка

Выберите версию, которая однозначно определяет ревизию исходного кода. Тег
`latest` автоматически не используется.

```shell
IMAGE_PREFIX=registry.example.com/team/online-market \
  ./build/production/build.sh 1.0.0
```

Скрипт записывает точные имена образов в `build/production/.images.env`.
Этот файл является артефактом сборки: перенесите его на промышленный сервер
вместе с Compose-файлом. Образы остаются локальными, если явно не передать
`PUSH=1`:

```shell
PUSH=1 IMAGE_PREFIX=registry.example.com/team/online-market \
  ./build/production/build.sh 1.0.0
```

Соберите gateway из соседнего репозитория и добавьте `GATEWAY_IMAGE` в
`.images.env` (или передайте переменную отдельно):

```shell
cd ../online_market_gateway
IMAGE_PREFIX=registry.example.com/team/online-market \
  PRODUCTION_VERSION=1.0.0 make production-build
```

По умолчанию `TARGET_PLATFORM` равен `linux/amd64`. Можно указать другую
одиночную Docker-платформу, например `linux/arm64`.

## Настройка

Создайте на промышленном сервере файл окружения, который не отслеживается Git:

```shell
cp build/production/.env.example build/production/.env
```

Укажите адреса PostgreSQL и Redis, два независимых секрета токенов, сети
доверенных прокси и реквизиты каналов уведомлений. Не добавляйте получившийся
файл в репозиторий. Имена образов читаются из артефакта `.images.env`,
созданного при сборке, включая `GATEWAY_IMAGE`.

Для обратной совместимости native gRPC публикуется на `127.0.0.1:8080`, а
HTTP/JSON gateway — на `127.0.0.1:8082`. Предполагается, что внешний сайт
направляет `/api/` на gateway и завершает TLS на обратном прокси-сервере на том
же узле. Порты настраиваются через `GRPC_PORT` и `GATEWAY_PORT`.

Gateway и API соединены отдельной внутренней сетью. Значение
`GATEWAY_NETWORK_SUBNET` должно входить в `TRUSTED_PROXY_CIDRS`; при наличии
внешнего TLS-прокси добавьте его точный адрес или подсеть в
`GATEWAY_TRUSTED_PROXY_CIDRS` у gateway и при необходимости в
`TRUSTED_PROXY_CIDRS` у API. Gateway сам не получает строки подключения к
хранилищам и секреты подписи токенов. По умолчанию соединение внутри этой
изолированной сети не использует TLS. Для межузлового размещения установите
`GATEWAY_GRPC_INSECURE=false`, настройте TLS на gRPC API и передайте доверенный
CA через отдельный Compose override.

Если сайт и API работают на одном origin, оставьте
`GATEWAY_ALLOWED_ORIGINS` пустым. Для разных origin перечислите только точные
разрешённые значения через запятую; wildcard не используется.

## Проверка и развёртывание

```shell
docker compose \
  --env-file build/production/.env \
  --env-file build/production/.images.env \
  -f build/production/docker-compose.yml \
  config --quiet

docker compose \
  --env-file build/production/.env \
  --env-file build/production/.images.env \
  -f build/production/docker-compose.yml \
  up -d
```
