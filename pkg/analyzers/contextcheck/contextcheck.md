# contextcheck

`context.Context` must propagate from a function's parameter, not be re-created
inside the body.

## What it catches

Creating `context.Background()` or `context.TODO()` inside a function that already
receives a `context.Context` breaks the cancellation/deadline chain. Requests hang
past their deadlines; cancellations never propagate to downstream calls. The bug
compiles cleanly and only surfaces under load.

It also reports calls that take no context although the function has one and
a context-aware variant exists:

| Call | Use instead |
|---|---|
| `db.Exec`, `Query`, `QueryRow`, `Prepare`, `Ping`, `Begin` on `*sql.DB`, `*sql.Tx`, `*sql.Conn`, `*sql.Stmt` | the `...Context` method |
| `http.NewRequest` | `http.NewRequestWithContext` |
| `http.Get`, `Post`, `Head`, `PostForm` (package or `*http.Client`) | `http.NewRequestWithContext` + `client.Do` |
| `exec.Command` | `exec.CommandContext` |
| `net.Dial`, `net.DialTimeout`, `Dialer.Dial` | `Dialer.DialContext` |

Without the context a query or request keeps running after the client
disconnected or the deadline passed.

## Bad

```go
func handle(ctx context.Context, req Req) {
    db.Query(context.Background(), req.Key) // ignores deadline from ctx
}

func (h *H) list(w http.ResponseWriter, r *http.Request) {
    rows, err := h.db.Query("select ...") // r.Context() is available
}
```

## Good

```go
func handle(ctx context.Context, req Req) {
    db.Query(ctx, req.Key)
}

func (h *H) list(w http.ResponseWriter, r *http.Request) {
    rows, err := h.db.QueryContext(r.Context(), "select ...")
}
```

## Opt-out

`// safe-ignore: <reason>` on the same line.

```go
go cleanup(context.Background()) // safe-ignore: detached cleanup must outlive request
```

## Limitations

- A function or function literal is a root only when its own signature
  includes a `context.Context` or `*http.Request` parameter. `main`, `init`, and helpers that
  take no context are not flagged.
- A nested literal that receives its own context is checked against that
  parameter. `context.Background()` inside it is reported. A nested literal
  with no context parameter is still part of the outer function: Background
  there drops the outer context.
- Calls inside a function literal started with `go` are not checked for a
  missing context: detached work must not inherit the caller's cancellation.
- Third-party clients (GORM's `WithContext`, Redis, ...) are not covered.
