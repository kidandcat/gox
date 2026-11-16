# goroutine

Goroutines must run in a scope with a visible lifecycle primitive.

## What it catches

`go f()` with no `errgroup`, `sync.WaitGroup`, or captured `context.CancelFunc`
spawns work that nobody waits for or can cancel. Errors panic the process; the
goroutine leaks past program shutdown; tests become flaky. This is the most
common goroutine bug pattern in unsupervised AI-generated code.

## Bad

```go
func handle(req Req) {
    go process(req) // who waits? who cancels?
}
```

## Good

```go
func handle(ctx context.Context, req Req) error {
    g, ctx := errgroup.WithContext(ctx)
    g.Go(func() error { return process(ctx, req) })
    return g.Wait()
}
```

## Opt-out

`// goroutine-ok: <reason>` on the same line as `go`.

```go
go metrics.Flush() // goroutine-ok: best-effort fire-and-forget, app already shutting down
```

## Limitations

- The `go` statement itself must refer to the primitive: `wg` / `g` / `cancel`
  in its call, arguments or literal body, or a `context.Context` when the
  same function holds a `CancelFunc`. A WaitGroup that only tracks other
  goroutines does not cover this one.
- A context received as a parameter is not enough on its own: gox cannot
  see whether the caller ever cancels it.
