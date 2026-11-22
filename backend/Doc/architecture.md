# Backend architecture

The backend is a modular Go gRPC application. Generated protobuf code is kept in
`backend/gen` and is used only at the transport boundary.

Browser clients use a separate HTTP/JSON gateway process from the
`online_market_gateway` repository. That gateway imports generated
`*.pb.gw.go` stubs from this repository and calls the native gRPC listener.
It does not call application services directly, so the complete gRPC
interceptor chain remains authoritative for authentication, authorization,
rate limiting, deadlines and error mapping.

## Modules

- `catalog` reads products and categories.
- `cart` owns anonymous server-side carts and validates product availability.
- `order` performs checkout, public order lookup and administrator mutations.
- `auth` manages administrator credentials and refresh sessions.
- `notification` delivers transactional outbox messages through channel
  implementations.
- `transport/grpcapi` maps protobuf requests to application services and maps
  domain errors to gRPC status codes.
- Generated `gen/online_market/v1/*.pb.gw.go` stubs are the contract surface
  consumed by `online_market_gateway`; HTTP mappings live in
  `api/proto/http/`.

PostgreSQL repositories implement the interfaces declared by application
modules. Business services do not accept protobuf messages or return generated
types.

Redis stores distributed rate-limit windows shared by all API replicas.
PostgreSQL remains the source of truth for catalog, carts, orders and sessions.

## Catalog administration

The public catalog API is read-only. Authenticated administrators manage
products and categories through `AdminCatalogService`. Product deletion archives
the product so existing carts and order history keep valid references. Category
deletion is rejected while a category has children or assigned products.

The initial migration inserts a small deterministic catalog so a clean MVP
deployment can exercise browsing, cart and checkout without manual SQL setup.

## Checkout transaction

Checkout serializes an idempotency key with a PostgreSQL advisory transaction
lock and then locks the cart row. The same transaction:

1. validates the cart and current products;
2. creates the order and item snapshots;
3. appends the initial audit event;
4. creates one outbox row per configured notification channel;
5. marks the cart as checked out.

Public order tokens are deterministically derived with HMAC from a dedicated
stable server secret and order ID. Only their SHA-256 hashes are stored. This allows an
idempotent checkout replay to return the same capability token without storing
the token itself.

## Order states

```text
new -> confirmed -> assembling -> ready -> in_delivery -> completed
 |        |             |          |
 +--------+-------------+----------+------------------------> cancelled
```

A ready pickup order can transition directly to `completed`. Completed and
cancelled orders are terminal and cannot be edited. An order already in
delivery cannot be cancelled through the MVP workflow.

## Notifications

The worker claims rows with `FOR UPDATE SKIP LOCKED`. Failed attempts are
rescheduled with exponential delay and become terminal after eight attempts.
Provider failures never roll back an already created order.

## gRPC middleware

Every unary RPC passes through the following interceptor chain:

1. request ID propagation or generation through `x-request-id` metadata;
2. structured completion logging without request or response bodies;
3. panic recovery with a stack trace;
4. a five-second maximum deadline while preserving shorter client deadlines;
5. Redis-backed rate limiting for selected public methods;
6. administrator authentication where required;
7. role authorization where required;
8. centralized application error mapping to public gRPC status codes.

Raw application errors are logged before mapping. PostgreSQL query errors are
logged by a PGX tracer without SQL arguments, which prevents passwords, tokens
and customer data from leaking through query parameters.

Rate limits use atomic Redis Lua scripts with sliding windows. Client IPs are
SHA-256 hashed before they are used in Redis keys. The direct gRPC peer is the
default source of the client IP; `x-forwarded-for` is accepted only from a
configured trusted proxy network.

The same five-second limit is applied to PostgreSQL startup checks, each
notification-worker iteration, Telegram HTTP calls and the administrator
creation command. Background notification logs do not have a request ID because
delivery happens asynchronously after the originating RPC has completed.

