# contextcheck

`context.Context` must propagate from a function's parameter, not be re-created
inside the body.

## What it catches

Creating `context.Background()` or `context.TODO()` inside a function that already
receives a `context.Context` breaks the cancellation/deadline chain. Requests hang
past their deadlines; cancellations never propagate to downstream calls. The bug
compiles cleanly and only surfaces under load.

## Bad

```go
func handle(ctx context.Context, req Req) {
    db.Query(context.Background(), req.Key) // ignores deadline from ctx
}
```

## Good

```go
func handle(ctx context.Context, req Req) {
    db.Query(ctx, req.Key)
}
```

## Opt-out

`// safe-ignore: <reason>` on the same line.

```go
go cleanup(context.Background()) // safe-ignore: detached cleanup must outlive request
```

## Limitations

- A function or function literal is a root only when its own signature
  includes a `context.Context` parameter. `main`, `init`, and helpers that
  take no context are not flagged.
- A nested literal that receives its own context is checked against that
  parameter. `context.Background()` inside it is reported. A nested literal
  with no context parameter is still part of the outer function: Background
  there drops the outer context.
