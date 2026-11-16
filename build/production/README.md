# Сборка и развёртывание в промышленной среде

Комплект для промышленной среды создаёт два неизменяемых образа:

- `online-market-api` содержит gRPC API и CLI создания администратора;
- `online-market-migrations` содержит зафиксированную версию go-migrate и
  SQL-миграции из репозитория.

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
созданного при сборке.

По умолчанию порт публикуется на `127.0.0.1:8080`. Предполагается, что TLS
завершается на обратном прокси-сервере на том же узле. Если API публикуется
напрямую, перед изменением адреса привязки настройте сертификат и ключ TLS через
отдельный файл переопределения развёртывания.

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

Контейнер миграций должен успешно завершить работу до запуска API. При ошибке
миграции API останется остановленным. Не изменяйте версию миграции принудительно,
не выяснив состояние базы данных.

## Создание первого администратора

Передайте данные первого администратора через вызывающую оболочку, чтобы пароль
не сохранялся в файле окружения:

```shell
export ADMIN_LOGIN='admin'
export ADMIN_DISPLAY_NAME='Administrator'
export ADMIN_ROLES='admin'
read -r -s ADMIN_PASSWORD
export ADMIN_PASSWORD

docker compose \
  --env-file build/production/.env \
  --env-file build/production/.images.env \
  -f build/production/docker-compose.yml \
  --profile tools run --rm admin-create

unset ADMIN_PASSWORD
```

Пароль администратора должен содержать не менее 12 символов.

## Откат

Для отката приложения укажите в `API_IMAGE` ранее опубликованный неизменяемый
тег. Миграции базы данных автоматически не откатываются: перед возвратом старой
версии приложения необходимо подтвердить её совместимость с текущей схемой.
