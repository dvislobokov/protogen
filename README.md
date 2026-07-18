<div align="center">

# 🧬 protogen

**One Go binary that turns `.proto` files into Go messages, gRPC, gRPC-gateway and OpenAPI v3 — with zero `protoc` and zero external plugins.**

Единый Go-бинарник, который превращает `.proto` в Go-сообщения, gRPC, gRPC-gateway и OpenAPI v3 — **без `protoc` и без внешних плагинов**.

[![CI](https://github.com/dvislobokov/protogen/actions/workflows/ci.yml/badge.svg)](https://github.com/dvislobokov/protogen/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/dvislobokov/protogen.svg)](https://pkg.go.dev/github.com/dvislobokov/protogen)
[![Go Report Card](https://goreportcard.com/badge/github.com/dvislobokov/protogen)](https://goreportcard.com/report/github.com/dvislobokov/protogen)
[![Go](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)
[![protoc](https://img.shields.io/badge/protoc-not_required-success)](#-why-it-works-without-protoc)
[![in-process](https://img.shields.io/badge/generation-in--process-brightgreen)](#-architecture)

**English** · [Русский](#-русский)

</div>

---

## ✨ Features

- 🚫 **No `protoc`, no `protoc-gen-*`** — nothing to install; parsing is pure Go ([`bufbuild/protocompile`](https://github.com/bufbuild/protocompile)).
- 🧱 **Messages** (`*.pb.go`) via the very same generator `protoc-gen-go` uses.
- 🔌 **gRPC** (`*_grpc.pb.go`) — unary **+ server / client / bidi streaming**.
- 🌐 **gRPC-gateway** (`*.pb.gw.go`) — REST↔gRPC reverse proxy, unary + server-streaming.
- 📖 **OpenAPI v3** (`openapi.yaml`), honoring `google.api.http` **and `openapi.v3.*` annotations** (document info, operation summary/tags, schema/property overrides).
- ✅ **Validation** with [protovalidate](https://github.com/bufbuild/protovalidate) — enforced at runtime **and reflected into the OpenAPI schema** (`minLength`, `pattern`, `format`, string `enum`, `readOnly`/`writeOnly`, `required`, …).
- 🩹 **ASP.NET Core-style 400** — validation failures become RFC 9457 `problem+json` with a field→messages map (and it's documented in OpenAPI too).
- 🔐 **Roles & permissions per method** — annotate RPCs with `(protogen.authz.requires)` (`any_of`/`all_of`/`none_of`) and enforce them with the bundled gRPC interceptors.
- 📦 **Bundled well-known imports** — `google/api/*` (incl. `field_behavior`), `buf/validate/*`, `openapiv3/*` and `protogen/authz/*` are embedded; no vendoring, no `--proto_path` for them.
- 🗂️ **Managed mode** — synthesizes `go_package`/`package` when your protos omit them.
- 🌳 **Monorepo-friendly** — point it at a directory (or glob) and it generates the whole tree at once.
- ⚙️ **Config file** — commit a `protogenall.yaml` instead of a wall of flags.
- 🔭 **Reflection + health** — `grpcx.Register(s)` adds server reflection and the health service.
- 🧰 **`go install`-able**, with `--version`.
- 🚀 **`protogenall init`** — scaffolds a starter proto (with all the annotations wired up) and a config; then a bare `protogenall` (or `protogenall <dir>`) generates everything.

## 🎯 Why it works without `protoc`

`protoc` is a C++ toolchain and its plugins are separate binaries you must install. `protogen` replaces that whole pipeline with in-process Go:

| Stage | How |
|-------|-----|
| Parse `.proto` | `bufbuild/protocompile` (pure-Go compiler) |
| Well-known types | `protocompile.WithStandardImports` (embedded) |
| Bundled imports | `google/api/*` + `buf/validate/*` + `openapiv3/*` are `go:embed`-ed and served by a composite resolver (`--list-builtins`) |
| Managed metadata | inject `go_package` / `package` from flags |
| Messages | `protoc-gen-go/internal_gengo` — importable (the path element is `internal_gengo`, not `internal`) |
| gRPC | compact `protogen` generator, modeled on protoc-gen-go-grpc's generics output |
| gRPC-gateway | own `protogen` generator + vendored `internal/gateway/httprule` for path-pattern compilation |
| OpenAPI v3 | `google/gnostic` generator |
| Validation → OpenAPI | `internal/openapival` post-processes the document |

> **The one subtlety:** protocompile materializes custom options (e.g. `google.api.http`) as `dynamicpb` messages that downstream generators can't read. `normalizeExtensions` re-encodes them through the global type registry into concrete Go types — exactly replicating the protoc↔plugin boundary. See `internal/compile/compile.go`.

## 📦 Install

```sh
go install github.com/dvislobokov/protogen/cmd/protogenall@latest
protogenall --version
```

Or run from a clone:

```sh
go run ./cmd/protogenall --help
```

## 🚀 Quick start

Scaffold a project and generate — two commands, zero flags:

```sh
protogenall init myapi     # creates myapi/proto/…/myapi.proto + myapi/protogenall.yaml
protogenall myapi          # or: cd myapi && protogenall
# → myapi/gen/…  (*.pb.go, *_grpc.pb.go, *.pb.gw.go, openapi.yaml)
```

`init` writes a starter proto that already demonstrates `google.api.http`, `buf.validate`, `openapi.v3` and `protogen.authz` annotations, plus a `protogenall.yaml`; run it inside a Go module and `go_package_prefix` is derived from your `go.mod`. Existing files are never overwritten (use `--force`).

It also vendors the builtin annotation protos into `third_party/` so IDE protobuf plugins can resolve the imports (point the plugin's import paths at `proto/` and `third_party/`). These files are for the IDE only: the compiler uses the copies embedded in the binary, and they are never code-generated even if they end up in the inputs.

Or drive everything with flags:

```sh
protogenall \
  --proto_path=example/proto \
  --go-package-prefix=example.com/gen \
  --openapi-title="Greeter API" \
  --out=gen \
  greeter.proto
# → gen/greeter.pb.go  gen/greeter_grpc.pb.go  gen/greeter.pb.gw.go  gen/openapi.yaml
```

`google/api/*`, `buf/validate/*` and `openapiv3/*` are bundled in the binary, so you point `--proto_path` only at **your** protos.

### 🌳 Whole monorepo at once

Inputs may be files, directories, or globs — a directory is walked recursively and output mirrors the source tree:

```sh
protogenall --proto_path=monorepo --go-package-prefix=example.com/gen --out=gen monorepo
```

### ⚙️ Config file

Commit a `protogenall.yaml` (auto-detected in the CWD, or `--config path`); explicit flags override it.

```yaml
proto_paths: [proto]
inputs: [proto]
out: gen
go_package_prefix: example.com/gen
openapi: { title: Checkout API, version: 1.0.0 }
generators: [messages, grpc, gateway, openapiv3]   # subset allowed; default all
```

## ✅ Validation → OpenAPI

Write [protovalidate](https://github.com/bufbuild/protovalidate) constraints in your proto:

```proto
message PlaceOrderRequest {
  string customer_email = 1 [(buf.validate.field).required = true, (buf.validate.field).string.email = true];
  Currency currency      = 5 [(buf.validate.field).enum = {defined_only: true, not_in: [0]}];
  repeated LineItem items = 6 [(buf.validate.field).repeated = {min_items: 1, max_items: 50}];
  string order_id        = 7 [(google.api.field_behavior) = OUTPUT_ONLY];
}
```

…and they show up in `openapi.yaml` (no separate validator is generated — protovalidate checks at runtime):

```yaml
customerEmail: { type: string, format: email }
currency:      { type: string, enum: [USD, EUR, GBP] }   # string names, as protojson serializes them
items:         { type: array, minItems: 1, maxItems: 50 }
orderId:       { type: string, readOnly: true }
required: [customerEmail, ...]
```

> Enums render as **string names** by default (matching grpc-gateway's JSON). Pass
> `--openapi-enum-format=number` (or `openapi.enum_format: number`) to get numeric
> `enum` values with an `x-enum-varnames` hint instead.

## 📝 OpenAPI annotations

Import `openapiv3/annotations.proto` (bundled — nothing to vendor) to customize the OpenAPI output right from the proto: document info, per-operation summary/description/tags, and schema/property overrides. These are [gnostic's `openapi.v3` options](https://github.com/google/gnostic/blob/main/openapiv3/annotations.proto), read natively by the OpenAPI generator:

```proto
import "openapiv3/annotations.proto";

option (openapi.v3.document) = {
  info: { title: "Pet Store API", version: "1.2.3", description: "..." }
};

service PetService {
  rpc GetPet(GetPetRequest) returns (Pet) {
    option (google.api.http) = { get: "/v1/pets/{id}" };
    option (openapi.v3.operation) = {
      summary: "Fetch a pet"
      tags: ["pets"]
    };
  }
}

message Pet {
  option (openapi.v3.schema) = { description: "A pet in the store." };
  string name = 1 [(openapi.v3.property) = { max_length: 64 }];
}
```

`(openapi.v3.document).info` takes precedence over `--openapi-title`/`--openapi-version`.

## 🔐 Roles & permissions per method

Annotate methods with `(protogen.authz.requires)` (the proto is bundled) and enforce it with the interceptors from `github.com/dvislobokov/protogen/authz`:

```proto
import "protogen/authz/authz.proto";

service Greeter {
  // Default for methods without their own annotation.
  option (protogen.authz.default_requires) = { public: true };

  rpc SayHello(HelloRequest) returns (HelloReply) {
    option (protogen.authz.requires) = {
      roles: { any_of: ["admin", "greeter"] }        // "oneOf" semantics
      permissions: { all_of: ["greetings.write"] }   // "all" semantics
    };
  }
}
```

Rules support `any_of` (at least one), `all_of` (every one) and `none_of` (deny-list); all set fields must pass. `{}` means "any authenticated subject", `{ public: true }` skips checks entirely, and unannotated methods (with no service default) are not checked.

Enforcement is one interceptor pair — you supply the `SubjectFunc` that extracts roles/permissions from the request (e.g. from a JWT):

```go
subject := func(ctx context.Context) (*authz.Subject, error) {
    claims, err := verifyJWT(ctx) // your auth
    if err != nil || claims == nil {
        return nil, err // nil subject → Unauthenticated on protected methods
    }
    return &authz.Subject{Roles: claims.Roles, Permissions: claims.Scopes}, nil
}

s := grpc.NewServer(
    grpc.ChainUnaryInterceptor(authz.UnaryServerInterceptor(subject)),
    grpc.ChainStreamInterceptor(authz.StreamServerInterceptor(subject)),
)
```

Failures map to gRPC codes (`Unauthenticated` / `PermissionDenied`), which the gateway translates to HTTP 401/403. `authz.Authorize(ctx, fullMethod, subject)` is exported for custom transports.

## 🌊 Streaming

All four RPC kinds are generated. `example/stream` exercises each end to end (bufconn), plus a gateway server-streaming HTTP round trip:

```proto
service Chat {
  rpc Send(Message) returns (Ack);                       // unary
  rpc Subscribe(SubscribeRequest) returns (stream Message) {  // server streaming → HTTP chunked JSON
    option (google.api.http) = { get: "/v1/rooms/{room}/messages" };
  }
  rpc Upload(stream Chunk) returns (UploadSummary);      // client streaming
  rpc Converse(stream Message) returns (stream Message); // bidi
}
```

Client/bidi streaming can't be expressed over REST, so the gateway skips them (with a note); server-streaming is forwarded as a chunked JSON stream.

## 🩹 ASP.NET Core-style validation errors

`rest.ProblemErrorHandler` (a `runtime.WithErrorHandler`) turns a protovalidate failure into RFC 9457 `problem+json`, with keys in JSON (camelCase) so they match the payload and OpenAPI schema — try `go run ./example/checkout`:

```json
HTTP 400  application/problem+json
{
  "type": "https://datatracker.ietf.org/doc/html/rfc9110#section-15.5.1",
  "title": "One or more validation errors occurred.",
  "status": 400,
  "errors": {
    "customerEmail": ["must be a valid email address"],
    "items":         ["must contain at least 1 item(s)"],
    "acceptTerms":   ["must equal true"]
  }
}
```

## 🔭 Reflection + health

```go
s := grpc.NewServer()
shop.RegisterCheckoutServer(s, impl{})
grpcx.Register(s) // server reflection + health service (SERVING)
```

## 🏗️ Architecture

```
.proto ─▶ protocompile ─▶ managed mode ─▶ CodeGeneratorRequest ─▶ generators (in-process)
         (no protoc)      (go_package)                            ├─ messages   (internal_gengo)
                                                                  ├─ grpc       (own)
                                                                  ├─ gateway    (own + httprule)
                                                                  └─ openapi v3 (gnostic)
                                                                       └─ openapival enrichment
```

New generators just implement the `gen.Generator` interface (`internal/gen/registry.go`) and are appended in `cmd/protogenall/main.go`.

## 🗺️ Roadmap

- 🔍 Breaking-change detection (pairs with `--descriptor-set-out`)
- 🔁 Dual-mode: run as a `protoc`/`buf` plugin (read `CodeGeneratorRequest` from stdin)
- 🧹 Proto linter

## 📄 License

Apache 2.0 — see [LICENSE](LICENSE). Bundles Apache-licensed protos (`google/api`, `buf/validate`, gnostic's `openapiv3`) and vendors `grpc-gateway`'s `httprule` package.

---

<div align="center">

## 🇷🇺 Русский

[English](#-protogen) · **Русский**

</div>

**`protogen`** — единый Go-бинарник, который генерирует Go-сообщения, gRPC-стабы, gRPC-gateway и OpenAPI v3 из `.proto` **без `protoc` и без внешних плагинов**. Вся генерация — in-process.

### ✨ Возможности

- 🚫 **Ни `protoc`, ни `protoc-gen-*`** — ставить нечего; парсинг на чистом Go ([`bufbuild/protocompile`](https://github.com/bufbuild/protocompile)).
- 🧱 **Сообщения** (`*.pb.go`) — тем же генератором, что и `protoc-gen-go`.
- 🔌 **gRPC** (`*_grpc.pb.go`) — unary **+ server / client / bidi стриминг**.
- 🌐 **gRPC-gateway** (`*.pb.gw.go`) — REST↔gRPC прокси, unary + server-streaming.
- 📖 **OpenAPI v3** (`openapi.yaml`) с учётом `google.api.http` **и аннотаций `openapi.v3.*`** (info документа, summary/tags операций, переопределения схем и полей).
- ✅ **Валидация** через [protovalidate](https://github.com/bufbuild/protovalidate) — проверка в рантайме **и отражение в OpenAPI-схему** (`minLength`, `pattern`, `format`, строковый `enum`, `readOnly`/`writeOnly`, `required`, …).
- 🩹 **Ошибки в стиле ASP.NET Core** — невалидный запрос превращается в RFC 9457 `problem+json` с картой поле→сообщения (и это описано в OpenAPI).
- 🔐 **Роли и пермиссии на метод** — аннотация `(protogen.authz.requires)` (`any_of`/`all_of`/`none_of`) + готовые gRPC-интерцепторы для проверки.
- 📦 **Встроенные well-known импорты** — `google/api/*` (в т.ч. `field_behavior`), `buf/validate/*`, `openapiv3/*` и `protogen/authz/*` вшиты; ни вендоринга, ни `--proto_path` для них.
- 🗂️ **Managed mode** — подставляет `go_package`/`package`, если их нет в proto.
- 🌳 **Дружит с монорепой** — укажи папку (или glob), и всё дерево сгенерируется за раз.
- ⚙️ **Конфиг-файл** — вместо простыни флагов коммить `protogenall.yaml`.
- 🔭 **Reflection + health** — `grpcx.Register(s)` добавляет server reflection и health-сервис.
- 🧰 **Ставится через `go install`**, есть `--version`.
- 🚀 **`protogenall init`** — скаффолдит стартовый proto (со всеми аннотациями) и конфиг; дальше достаточно `protogenall` без аргументов (или `protogenall <папка>`).

### 🎯 Почему это работает без `protoc`

`protoc` — это C++-тулчейн, а его плагины — отдельные бинарники, которые надо ставить. `protogen` заменяет весь этот конвейер на in-process Go:

| Этап | Как |
|------|-----|
| Парсинг `.proto` | `bufbuild/protocompile` (компилятор на чистом Go) |
| Well-known типы | `protocompile.WithStandardImports` (встроены) |
| Встроенные импорты | `google/api/*` + `buf/validate/*` + `openapiv3/*` через `go:embed` и композитный резолвер (`--list-builtins`) |
| Метаданные | подстановка `go_package` / `package` из флагов |
| Сообщения | `protoc-gen-go/internal_gengo` — импортируется (элемент пути — `internal_gengo`, не `internal`) |
| gRPC | компактный `protogen`-генератор по образцу generics-вывода protoc-gen-go-grpc |
| gRPC-gateway | свой `protogen`-генератор + завендоренный `internal/gateway/httprule` |
| OpenAPI v3 | генератор `google/gnostic` |
| Валидация → OpenAPI | пост-обработка в `internal/openapival` |

> **Единственная тонкость:** protocompile материализует кастомные опции (например `google.api.http`) как `dynamicpb`-сообщения, которые генераторы не читают. `normalizeExtensions` переупаковывает их через глобальный реестр типов в конкретные Go-типы — ровно как это происходит на границе protoc↔plugin. См. `internal/compile/compile.go`.

### 📦 Установка

```sh
go install github.com/dvislobokov/protogen/cmd/protogenall@latest
protogenall --version
```

### 🚀 Быстрый старт

Скаффолдинг и генерация — две команды без флагов:

```sh
protogenall init myapi     # создаст myapi/proto/…/myapi.proto + myapi/protogenall.yaml
protogenall myapi          # или: cd myapi && protogenall
# → myapi/gen/…  (*.pb.go, *_grpc.pb.go, *.pb.gw.go, openapi.yaml)
```

`init` кладёт стартовый proto с уже подключёнными аннотациями (`google.api.http`, `buf.validate`, `openapi.v3`, `protogen.authz`) и `protogenall.yaml`; внутри Go-модуля `go_package_prefix` берётся из `go.mod`. Существующие файлы не перезаписываются (есть `--force`).

Дополнительно `init` кладёт копии встроенных аннотационных proto в `third_party/`, чтобы protobuf-плагин IDE резолвил импорты и давал подсказки (укажите в плагине import paths `proto/` и `third_party/`). Эти файлы нужны только IDE: компилятор использует копии, встроенные в бинарник, и код по ним никогда не генерируется, даже если они попали во inputs.

Или всё то же флагами:

```sh
protogenall \
  --proto_path=example/proto \
  --go-package-prefix=example.com/gen \
  --openapi-title="Greeter API" \
  --out=gen \
  greeter.proto
```

`google/api/*`, `buf/validate/*` и `openapiv3/*` вшиты в бинарник, поэтому `--proto_path` указывает только на **твои** proto.

### 🌳 Вся монорепа за раз

На вход можно подать файлы, папки или glob-и; папка обходится рекурсивно, вывод зеркалит структуру:

```sh
protogenall --proto_path=monorepo --go-package-prefix=example.com/gen --out=gen monorepo
```

### ⚙️ Конфиг-файл

Коммить `protogenall.yaml` (авто-детект в CWD или `--config path`); явные флаги переопределяют его.

```yaml
proto_paths: [proto]
inputs: [proto]
out: gen
go_package_prefix: example.com/gen
openapi: { title: Checkout API, version: 1.0.0 }
generators: [messages, grpc, gateway, openapiv3]   # можно подмножество; по умолчанию все
```

### ✅ Валидация → OpenAPI

Пишешь констрейнты [protovalidate](https://github.com/bufbuild/protovalidate) в proto — и они появляются в `openapi.yaml` (отдельный валидатор не генерируется, protovalidate проверяет в рантайме): форматы (`email`/`uuid`), `pattern`, диапазоны, строковый `enum` (имена значений, как их сериализует grpc-gateway; `--openapi-enum-format=number` вернёт числа), `readOnly`/`writeOnly` (из `field_behavior`), `required`.

### 📝 OpenAPI-аннотации

Импортируй `openapiv3/annotations.proto` (вшит в бинарник) и настраивай OpenAPI прямо из proto: `(openapi.v3.document)` — info документа, `(openapi.v3.operation)` — summary/description/tags операции, `(openapi.v3.schema)` и `(openapi.v3.property)` — переопределения схем и полей. Это [опции `openapi.v3` из gnostic](https://github.com/google/gnostic/blob/main/openapiv3/annotations.proto); `(openapi.v3.document).info` имеет приоритет над `--openapi-title`/`--openapi-version`.

### 🔐 Роли и пермиссии на метод

Импортируй `protogen/authz/authz.proto` (вшит) и вешай на методы `(protogen.authz.requires)` с правилами `any_of` (хотя бы одна — «oneOf»), `all_of` (все — «all») и `none_of` (запрещённые); `(protogen.authz.default_requires)` на сервисе задаёт дефолт. `{}` — «любой аутентифицированный», `{ public: true }` — без проверок, метод без аннотаций не проверяется. Enforcement — интерцепторы `authz.UnaryServerInterceptor` / `authz.StreamServerInterceptor` из `github.com/dvislobokov/protogen/authz`: ты передаёшь `SubjectFunc`, достающий роли/пермиссии из контекста (например из JWT), а отказ маппится в `Unauthenticated`/`PermissionDenied` (401/403 через gateway). Пример — `example/proto/greeter.proto` и `authz/interceptor_test.go`.

### 🌊 Стриминг

Генерируются все четыре вида RPC. `example/stream` гоняет каждый end-to-end (bufconn) + HTTP server-streaming через gateway. Client/bidi по REST невыразимы — gateway их пропускает; server-streaming форвардится как chunked JSON.

### 🩹 Ошибки в стиле ASP.NET Core

`rest.ProblemErrorHandler` превращает провал валидации в RFC 9457 `problem+json` с ключами в camelCase (совпадают с телом запроса и OpenAPI). Запусти `go run ./example/checkout`:

```json
HTTP 400  application/problem+json
{
  "title": "One or more validation errors occurred.",
  "status": 400,
  "errors": {
    "customerEmail": ["must be a valid email address"],
    "items":         ["must contain at least 1 item(s)"]
  }
}
```

### 🔭 Reflection + health

```go
s := grpc.NewServer()
shop.RegisterCheckoutServer(s, impl{})
grpcx.Register(s) // server reflection + health (SERVING)
```

### 🗺️ Планы

- 🔍 Детектор breaking changes (пара к `--descriptor-set-out`)
- 🔁 Dual-mode: работать как плагин `protoc`/`buf` (читать `CodeGeneratorRequest` из stdin)
- 🧹 Proto-линтер

### 📄 Лицензия

Apache 2.0 — см. [LICENSE](LICENSE).

---

<div align="center">
<sub>Built to show that the whole proto → Go/gRPC/gateway/OpenAPI pipeline can live in a single dependency-free-at-runtime Go binary.</sub>
</div>
