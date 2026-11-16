# Backend configuration

The API reads configuration from environment variables.

## Required variables

- `DATABASE_URL` — PostgreSQL connection string.
- `REDIS_URL` — Redis connection URL. Redis stores distributed rate-limit
  windows shared by every API replica.
- `ACCESS_TOKEN_SECRET` — random secret containing at least 32 bytes. It signs
  administrator access tokens.
- `ORDER_TOKEN_SECRET` — a separate stable random secret containing at least 32
  bytes. It derives public order capability tokens and must not be rotated
  without a migration plan for idempotent checkout responses.

## Optional variables

- `GRPC_ADDRESS` — listen address, default `:8080`.
- `TLS_CERT_FILE` and `TLS_KEY_FILE` — optional server certificate and private
  key. Both must be set together. Production traffic carrying tokens or
  personal data must use this TLS mode or a trusted TLS-terminating proxy.
- `ACCESS_TOKEN_TTL` — administrator access token lifetime, default `15m`.
- `REFRESH_TOKEN_TTL` — refresh token lifetime, default `720h`.
- `CART_TTL` — anonymous cart lifetime, default `168h`.
- `NOTIFICATION_CHANNELS` — comma-separated channels. MVP supports `telegram`.
- `NOTIFICATION_POLL_INTERVAL` — outbox polling interval, default `2s`.
- `TRUSTED_PROXY_CIDRS` — optional comma-separated proxy networks. The API
  reads `x-forwarded-for` only when the direct gRPC peer belongs to one of these
  networks. The trusted proxy must sanitize or append this header correctly.
  Without this setting rate limits use the direct peer IP.
- `TELEGRAM_BOT_TOKEN` and `TELEGRAM_CHAT_ID` — required when the `telegram`
  channel is enabled.

## Initial setup

Apply the initial migration:

```shell
make -C backend migrate-up DATABASE_URL='postgres://...'
```

Create the first administrator without putting the password in command history:

```shell
export DATABASE_URL='postgres://...'
export ADMIN_LOGIN='admin'
export ADMIN_PASSWORD='use-a-long-random-password'
export ADMIN_DISPLAY_NAME='Administrator'
export ADMIN_ROLES='admin'
make -C backend admin-create
```

The password is stored only as a bcrypt hash. Cart, order and refresh tokens are
stored only as SHA-256 hashes. Do not log the environment variables above.

## Running

```shell
export REDIS_URL='redis://localhost:6379/0'
make -C backend run
```

All methods of `AdminOrderService` and `AdminCatalogService` require the
following gRPC metadata:

```text
authorization: Bearer <access-token>
```

The standard gRPC health service is registered on the same listener.

Catalog and cart services have independent shared limits of 100 requests per
minute and 30 requests per 10 seconds for each client IP. `CreateOrder` uses the
same limits in its own bucket. `RefreshSession` allows 10 requests per minute
per client IP. Administrative order methods are not rate limited. A rejected
request returns `ResourceExhausted` with `google.rpc.RetryInfo`.
Limited methods fail closed with `Unavailable` when Redis cannot be reached;
administrative order methods remain independent from the limiter.

For local Docker deployment, copy `build/.env.example` to an untracked `.env`
and fill `POSTGRES_PASSWORD`, `ACCESS_TOKEN_SECRET` and `ORDER_TOKEN_SECRET`.
Both token secrets must contain at least 32 bytes. Then run:

```shell
make backend-local-up
```

The command starts PostgreSQL, Redis, applies the migration, builds the API and
starts grpc-ui. Open `http://127.0.0.1:8081`; the gRPC endpoint itself is
available at `localhost:8080`. The grpc-ui port can be changed with
`GRPCUI_PORT` in `build/.env`.

Administrative methods require metadata in the grpc-ui request form:

```text
authorization: Bearer <access-token returned by Login>
```

Useful commands:

```shell
make backend-local-logs
make backend-local-ps
make backend-local-down
```

