---
name: zen
description: Reference for the zen Go web framework (github.com/SoftKiwiGames/zen) — an opinionated framework for building Go HTTP APIs and UIs with sensible defaults. Use this skill whenever the user is writing, reading, debugging, or scaffolding Go code that imports `github.com/SoftKiwiGames/zen/zen`, `zen/pg`, or `zen/sqlite`, or whenever they mention "zen framework", `zen.NewHttpServer`, `zen.Resource`, `zen.APIResource`, zen sessions, zen cookies, zen embeds, or `zen init`. Also use it when the user is building a Go web service and the conversation has established that zen is the framework in play, even if they don't name it in the current message. Don't assume standard library, chi, gin, or echo idioms apply — zen wraps chi but exposes its own API, response helpers, middleware, and resource conventions that differ from those frameworks.
---

# zen — Go web framework

zen is an opinionated Go web framework wrapping chi, with built-in helpers for sessions, cookies, password hashing, validation, embedded static assets, Postgres, SQLite, Redis, NATS/JetStream, S3, and SMTP. It targets API + UI services where sensible defaults matter more than maximum flexibility.

When working with zen code, prefer the framework's own helpers over rolling equivalents from the standard library or other libraries — that's the whole point of the framework, and mixing idioms produces inconsistent code.

## Package layout

There are three import paths and they have distinct purposes:

- `github.com/SoftKiwiGames/zen/zen` — everything except the database drivers. HTTP server, router, middleware, sessions, cookies, passwords, validation, embeds, NATS, S3, SMTP, Redis client, utilities.
- `github.com/SoftKiwiGames/zen/zen/pg` — PostgreSQL driver built on pgx, with goose migrations.
- `github.com/SoftKiwiGames/zen/zen/sqlite` — SQLite driver (CGO-free via modernc.org/sqlite), WAL mode, separate read/write pools, goose migrations.

Install with `go get github.com/SoftKiwiGames/zen`. For project scaffolding, `go get -tool github.com/SoftKiwiGames/zen` then `go tool zen init`.

## Minimal server

```go
package main

import (
    "context"
    "net/http"
    "github.com/SoftKiwiGames/zen/zen"
)

func main() {
    envs := zen.LoadEnvs("ADDR")
    srv := zen.NewHttpServer(&zen.Options{
        AllowedHosts: []string{"localhost:4000"},
        CorsOrigins:  []string{"http://localhost:4000"},
        SSL:          false,
    })

    srv.Get("/api/hello", func(w http.ResponseWriter, r *http.Request) {
        zen.HttpOk(w, map[string]string{"message": "hello"})
    })

    srv.Run(context.Background(), envs)
}
```

`NewHttpServer` already attaches: strip-slashes, `/_/health` heartbeat, request logging, panic recovery, request IDs, security headers (HSTS, X-Frame-Options, X-Content-Type-Options, referrer policy, permissions policy), CORS, and a 50 MB max request size. Don't re-add these.

`Options` is just three fields:

```go
type Options struct {
    CorsOrigins  []string
    AllowedHosts []string
    SSL          bool
}
```

`HttpServer.Run(ctx, envs)` reads `ADDR` from envs (defaulting to `:4000`) and handles graceful shutdown with a 15-second timeout.

## Router

The `Router` wraps chi. Standard verbs are all there: `Get`, `Post`, `Put`, `Patch`, `Delete`, `Head`, `Options`, `Connect`, `Trace`. Nested groups use `Group(path string, fn func(*Router))`. Middleware attaches with `Use(middleware Middleware)` where `Middleware` is `func(http.Handler) http.Handler`. Mount embedded static files with `Embeds(path string, embeds *Embedded)`.

### RESTful resources

zen has a Rails-style resource convention. There are three flavors:

- `srv.Resource("/posts", &PostResource{})` — full HTML resource with `Index`, `Show`, `New`, `Create`, `Edit`, `Update`, `Destroy`.
- `srv.APIResource("/api/posts", &PostAPIResource{})` — API resource with `Index`, `Show`, `Create`, `Update`, `Destroy` (no `New`/`Edit` since those are HTML form pages).
- `srv.PartialAPIResource("/products", products, zen.APIResourceRoutes{Index: true})` — pick which routes you want.

Embed `zen.Resource` or `zen.APIResource` in your struct to get default no-op implementations of every method, then override only the ones you need:

```go
type ProductResource struct {
    zen.APIResource
}

func (res *ProductResource) Index(w http.ResponseWriter, r *http.Request) {
    zen.HttpOk(w, []string{"A", "B", "C"})
}
```

The full interfaces:

```go
type HttpResource interface {
    Index(w http.ResponseWriter, r *http.Request)
    Show(w http.ResponseWriter, r *http.Request)
    New(w http.ResponseWriter, r *http.Request)
    Create(w http.ResponseWriter, r *http.Request)
    Edit(w http.ResponseWriter, r *http.Request)
    Update(w http.ResponseWriter, r *http.Request)
    Destroy(w http.ResponseWriter, r *http.Request)
}

type HttpAPIResource interface {
    Index(w http.ResponseWriter, r *http.Request)
    Show(w http.ResponseWriter, r *http.Request)
    Create(w http.ResponseWriter, r *http.Request)
    Update(w http.ResponseWriter, r *http.Request)
    Destroy(w http.ResponseWriter, r *http.Request)
}
```

## HTTP response helpers

Always reach for these instead of writing JSON by hand. They all set `Content-Type: application/json; charset=utf-8`.

| Helper | Status | Body |
|---|---|---|
| `HttpOk(w, data)` | 200 | JSON of `data` |
| `HttpOkDefault(w)` | 200 | `{"ok": true}` |
| `HttpCreated(w, data)` | 201 | JSON of `data` |
| `HttpCreatedDefault(w)` | 201 | empty |
| `HttpNoContent(w)` | 204 | empty |
| `HttpBadRequest(w, err, humanError)` | 400 | `{"error": humanError}` |
| `HttpBadRequestWithCode(w, err, code)` | 400 | `{"error": ..., "code": code}` |
| `HttpUnauthorized(w)` | 401 | — |
| `HttpForbidden(w)` | 403 | — |
| `HttpNotFound(w)` | 404 | — |
| `HttpErrorConflict(w, message)` | 409 | with message |
| `HttpInternalServerError(w, message)` | 500 | with message |
| `HttpInternalServerErrorDefault(w)` | 500 | generic message |

## Middleware

Built-in middleware lives in the `zen` package. Each returns a `Middleware` (i.e. `func(http.Handler) http.Handler`) you attach with `Use`.

```go
// HTTP Basic Auth, constant-time comparison
MiddlewareBasicAuth(user, password, realm string) Middleware

// Session-based auth — reads session cookie and loads the session into context
MiddlewaRequireAuth(store *SessionManager, cookieName string) Middleware  // note: typo "Middlewa" is the actual name

// Limit request body size
MiddlewareMaxRequestSize(bytes int64) Middleware

// Per-IP rate limiting
MiddlewareRateLimitByIP(requestLimit int, windowLength time.Duration) Middleware

// Request timeout
MiddlewareTimeout(duration time.Duration) Middleware

// Load an arbitrary resource into context under a key
WithResource(key any, getter func(*http.Request) (any, error)) Middleware
```

Note that the auth-required middleware is spelled `MiddlewaRequireAuth` (not `MiddlewareRequireAuth`) — keep that spelling when writing zen code; don't "correct" it.

## Sessions

Sessions are 7-day, UUIDv7-keyed, and use a pluggable store.

```go
type Session struct {
    ID        string
    AccountID uuid.UUID
    Email     string
    Role      string
    CreatedAt time.Time
    ExpiresAt time.Time
}

store := zen.NewMemorySessionStore()  // or zen.NewRedisSessionStore(...)
manager := zen.NewSessionManager(store)

session, err := manager.Create(ctx, accountID, email, role)
session, err := manager.Get(ctx, sessionID)
err := manager.Delete(ctx, sessionID)
```

Inside a handler that ran behind `MiddlewaRequireAuth`, retrieve the session from request context with `zen.CtxGetSession(ctx) *Session`.

To plug in a custom backing store, implement `SessionStore`:

```go
type SessionStore interface {
    SessionGet(ctx context.Context, key string) ([]byte, error)
    SessionSet(ctx context.Context, key string, data []byte, ttl time.Duration) error
    SessionDel(ctx context.Context, key string) error
}
```

## Cookies

Cookies are stored as base64-encoded JSON of a `CookieData` (`map[string]string`). Defaults are HttpOnly, SameSite=Lax, 7-day max-age.

```go
data, err := zen.CookieRead(r, name)
zen.CookieWrite(w, name, data, opts...)
zen.CookieExpire(w, name, opts...)
```

Options: `zen.CookieSecure(bool)`, `zen.CookieMaxAge(seconds int)`. Constants: `CookieDefaultSessionName = "zen_session"`, `CookieMaxAgeWeek`, `CookieMaxAgeDay`.

## Passwords

Uses scrypt with OWASP 2024 parameters (N=131072, r=8, p=1, 32-byte key). Hashes serialize as `base64(salt)||base64(hash)`.

```go
hash, err := zen.PasswordHash("mysecret")
ok, err := zen.PasswordVerify("mysecret", hash)
token, err := zen.PasswordRandom()  // 18 random bytes, base64url, useful for password-reset tokens
```

Do not roll your own bcrypt/argon2 here — use these.

## Validation

Reads JSON from a reader and validates with go-playground/validator struct tags in one call:

```go
parsed, err := zen.ParseAndValidateJSON[MyStruct](r.Body)
```

## Pagination

Parses `?page=1&limit=20` query params. Defaults to page 1, limit 20, capped at 50.

```go
meta := zen.ParsePagination(r)
meta.Page          // int64
meta.Limit         // int64
meta.Offset()      // for SQL OFFSET
meta.Pages(total)  // total page count given the total row count
```

## Environment variables

`LoadEnvs` reads `.env` automatically and returns a typed bag.

```go
envs := zen.LoadEnvs("ADDR", "DATABASE_URL", "REDIS_URL")
addr := envs.Get("ADDR")              // panics if missing
addr := envs.GetOr("ADDR", ":4000")   // fallback if missing or empty
```

## URL parameters

```go
id := zen.ParamID(r, "id")    // uuid.UUID, returns uuid.Nil on parse error
id := zen.ParamUUID(r, "id")  // alias for ParamID
val := zen.Param(r, "name")   // raw string param
```

## Embedding static files

```go
//go:embed ui/dist
var ui embed.FS

embeds, err := zen.NewEmbeds(ui, "/ui/dist")
srv.Embeds("/", embeds)
```

Files served via `Embeds` automatically get SHA-256 ETags, `Cache-Control: public, max-age=7776000, immutable`, proper MIME types, and `If-None-Match` 304 handling. Don't write a custom static handler for this — use `Embeds`.

## PostgreSQL — `zen/pg`

```go
db := pg.NewPostgres(uri, migrationsFS)
err := db.Connect(ctx)
err := db.MigrateWithGoose(ctx, "db/migrations")
err := db.Ping(ctx)
db.Close(ctx)

// Queries
rows, err := db.Query(ctx, "SELECT ...", args...)
row := db.QueryRow(ctx, "SELECT ...", args...)
tag, err := db.Exec(ctx, "INSERT ...", args...)

// Transactions
err := db.Transaction(ctx, func(tx pgx.Tx) error {
    q := db.WithTx(tx)
    q.Exec(ctx, "...")
    return nil
})
```

Helpers worth knowing:

- `pg.ID(uuid)` → `pgtype.UUID`
- `pg.Text(str)` → `pgtype.Text`
- `pg.NoRows(err)` → true if `pgx.ErrNoRows`
- `pg.UniqueViolation(err)` → true if PG error code 23505

Use `pg.NoRows` and `pg.UniqueViolation` for error checks rather than string-matching error messages or comparing against `pgx.ErrNoRows` directly.

## SQLite — `zen/sqlite`

Separate read pool (20 conns) and write pool (1 conn) with WAL mode — this is the standard pattern for SQLite under load and zen handles it for you.

```go
db := sqlite.NewSQLite(uri, migrationsFS)
err := db.Connect(ctx)
err := db.MigrateWithGoose(ctx, "db/migrations")
err := db.Ping(ctx)
db.Close(ctx)
```

Same query interface as `pg`: `Exec`, `Query`, `QueryRow`, `Transaction`, `WithTx`.

Helpers:

- `sqlite.NoRows(err)` — true if `sql.ErrNoRows`
- `sqlite.UniqueViolation(err)` — true if `UNIQUE constraint failed`
- `sqlite.Date(t)` / `sqlite.Time(t)` / `sqlite.Timestamp(t)` — format `time.Time` for storage
- `sqlite.FromDate(s)` / `sqlite.FromTime(s)` / `sqlite.FromTimestamp(s)` — parse back

`Timestamp` uses RFC3339Nano. Use these helpers for time storage in SQLite — don't store raw `time.Time` strings or unix epochs ad hoc.

## Redis

```go
r := &zen.Redis{}
cleanup, err := r.Connect(redisURL)
defer cleanup()
r.Client  // *redis.Client (go-redis/v9)
```

## NATS / JetStream

```go
n := zen.NewNATS(zen.Config{
    URL:            "nats://localhost:4222",
    Name:           "my-service",
    InitialStreams: []string{"events"},
})
err := n.Connect(ctx)
defer n.Close()

err := n.Publish(ctx, "events.user.created", myEvent)
n.JetStream()  // jetstream.JetStream — for setting up consumers
n.Conn()       // *nats.Conn — for raw NATS operations
```

## AWS S3

Works with real AWS or S3-compatible services (set `Endpoint` and `UsePathStyle` for the latter).

```go
client := &zen.AWSClient{
    Region:          "us-east-1",
    AccessKeyID:     "...",
    SecretAccessKey: "...",
    Endpoint:        "...",  // optional
    UsePathStyle:    false,
}

s3, err := client.S3(ctx, "my-bucket")
err := s3.PutObject(ctx, zen.PutObjectInput{Key: "file.txt", ContentType: "text/plain", Reader: r})
err := s3.PutObjectMulti(ctx, objects)         // concurrent upload
err := s3.GetObject(ctx, "file.txt", writer)
url, err := s3.GetPresignURL(ctx, "file.txt", 15*time.Minute)
```

## SMTP

Reads `SMTP_HOST`, `SMTP_PORT`, `SMTP_USER`, `SMTP_PASSWORD`, `SMTP_FROM` from envs.

```go
client := zen.New(envs)
err := client.Send(to, subject, htmlBody, textBody)
```

## Utilities

- `zen.MustURLJoinPath(base, elements...)` — panics on error (use when paths are static)
- `zen.JsonObject(data)` — `[]byte`, returns `{}` on marshal error
- `zen.JsonArray(data)` — `[]byte`, returns `[]` on marshal error
- `zen.MimeType(path)` — MIME type from extension
- `zen.CtxResource[T](ctx, key)` — type-safe context value access (pairs with the `WithResource` middleware)
- `zen.PeriodicallyUntilSuccess(ctx, period, timeout, fn)` — retry helper
- `zen.EmptyFS{}` — no-op `fs.FS`, useful when migrations are empty in tests

## Underlying dependencies

For context when debugging or reading stack traces: chi (router), pgx (PostgreSQL), modernc.org/sqlite (CGO-free SQLite), go-redis/v9 (Redis), nats.go (NATS/JetStream), aws-sdk-go-v2 (S3), goose (migrations), go-playground/validator (validation), unrolled/secure (security headers), `golang.org/x/crypto/scrypt` (passwords), gomail (SMTP), godotenv (.env loading).
