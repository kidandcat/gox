# lostcancel

Reports a cancel function from `context.WithCancel`, `WithTimeout`,
`WithDeadline` (or their `Cause` variants) that is discarded or never used.

## What it catches

The derived context holds a timer and a goroutine-visible child entry in its
parent until someone calls cancel. Dropping cancel leaks both until the parent
is done, which for `context.Background()` is never.

## Bad

```go
ctx, _ := context.WithTimeout(ctx, 5*time.Second)

ctx, cancel := context.WithCancel(ctx)
doWork(ctx) // cancel never referenced
```

## Good

```go
ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
defer cancel()
```

## Opt-out

`// safe-ignore: <reason>` on the assignment line.

## Limitations

- go vet's lostcancel checks every return path. This rule only checks that
  cancel is referenced somewhere in the function, so a cancel called on one
  branch and skipped on another is not reported.
- Function literals are checked as part of the function that contains them.
