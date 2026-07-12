<div align="center">

# 🧬 protogen

**One Go binary that turns `.proto` files into Go messages, gRPC, gRPC-gateway and OpenAPI v3 — with zero `protoc` and zero external plugins.**

Единый Go-бинарник, который превращает `.proto` в Go-сообщения, gRPC, gRPC-gateway и OpenAPI v3 — **без `protoc` и без внешних плагинов**.

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
- 📖 **OpenAPI v3** (`openapi.yaml`), honoring `google.api.http`.
- ✅ **Validation** with [protovalidate](https://github.com/bufbuild/protovalidate) — enforced at runtime **and reflected into the OpenAPI schema** (`minLength`, `pattern`, `format`, string `enum`, `readOnly`/`writeOnly`, `required`, …).
- 🩹 **ASP.NET Core-style 400** — validation failures become RFC 9457 `problem+json` with a field→messages map (and it's documented in OpenAPI too).
- 📦 **Bundled well-known imports** — `google/api/*` (incl. `field_behavior`) and `buf/validate/*` are embedded; no vendoring, no `--proto_path` for them.
- 🗂️ **Managed mode** — synthesizes `go_package`/`package` when your protos omit them.
- 🌳 **Monorepo-friendly** — point it at a directory (or glob) and it generates the whole tree at once.
- ⚙️ **Config file** — commit a `protogenall.yaml` instead of a wall of flags.
- 🔭 **Reflection + health** — `grpcx.Register(s)` adds server reflection and the health service.
- 🧰 **`go install`-able**, with `--version`.

## 🎯 Why it works without `protoc`

`protoc` is a C++ toolchain and its plugins are separate binaries you must install. `protogen` replaces that whole pipeline with in-process Go:

| Stage | How |
|-------|-----|
| Parse `.proto` | `bufbuild/protocompile` (pure-Go compiler) |
| Well-known types | `protocompile.WithStandardImports` (embedded) |
| Bundled imports | `google/api/*` + `buf/validate/*` are `go:embed`-ed and served by a composite resolver (`--list-builtins`) |
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

```sh
protogenall \
  --proto_path=example/proto \
  --go-package-prefix=example.com/gen \
  --openapi-title="Greeter API" \
  --out=gen \
  greeter.proto
# → gen/greeter.pb.go  gen/greeter_grpc.pb.go  gen/greeter.pb.gw.go  gen/openapi.yaml
```

`google/api/*` and `buf/validate/*` are bundled in the binary, so you point `--proto_path` only at **your** protos.

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

Apache 2.0 — see [LICENSE](LICENSE). Bundles Apache-licensed protos (`google/api`, `buf/validate`) and vendors `grpc-gateway`'s `httprule` package.

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
- 📖 **OpenAPI v3** (`openapi.yaml`) с учётом `google.api.http`.
- ✅ **Валидация** через [protovalidate](https://github.com/bufbuild/protovalidate) — проверка в рантайме **и отражение в OpenAPI-схему** (`minLength`, `pattern`, `format`, строковый `enum`, `readOnly`/`writeOnly`, `required`, …).
- 🩹 **Ошибки в стиле ASP.NET Core** — невалидный запрос превращается в RFC 9457 `problem+json` с картой поле→сообщения (и это описано в OpenAPI).
- 📦 **Встроенные well-known импорты** — `google/api/*` (в т.ч. `field_behavior`) и `buf/validate/*` вшиты; ни вендоринга, ни `--proto_path` для них.
- 🗂️ **Managed mode** — подставляет `go_package`/`package`, если их нет в proto.
- 🌳 **Дружит с монорепой** — укажи папку (или glob), и всё дерево сгенерируется за раз.
- ⚙️ **Конфиг-файл** — вместо простыни флагов коммить `protogenall.yaml`.
- 🔭 **Reflection + health** — `grpcx.Register(s)` добавляет server reflection и health-сервис.
- 🧰 **Ставится через `go install`**, есть `--version`.

### 🎯 Почему это работает без `protoc`

`protoc` — это C++-тулчейн, а его плагины — отдельные бинарники, которые надо ставить. `protogen` заменяет весь этот конвейер на in-process Go:

| Этап | Как |
|------|-----|
| Парсинг `.proto` | `bufbuild/protocompile` (компилятор на чистом Go) |
| Well-known типы | `protocompile.WithStandardImports` (встроены) |
| Встроенные импорты | `google/api/*` + `buf/validate/*` через `go:embed` и композитный резолвер (`--list-builtins`) |
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

```sh
protogenall \
  --proto_path=example/proto \
  --go-package-prefix=example.com/gen \
  --openapi-title="Greeter API" \
  --out=gen \
  greeter.proto
```

`google/api/*` и `buf/validate/*` вшиты в бинарник, поэтому `--proto_path` указывает только на **твои** proto.

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
