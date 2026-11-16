# waitgroup

Reports `sync.WaitGroup.Add` called inside the goroutine it is meant to count.

## What it catches

`wg.Wait()` can run before the new goroutine is scheduled. The counter is
still zero, so Wait returns immediately and the work is not waited for. The
code passes tests that happen to schedule the goroutine first.

## Bad

```go
for _, j := range jobs {
    go func() {
        wg.Add(1)
        defer wg.Done()
        run(j)
    }()
}
wg.Wait()
```

## Good

```go
for _, j := range jobs {
    wg.Add(1)
    go func() {
        defer wg.Done()
        run(j)
    }()
}

// Go 1.25+
for _, j := range jobs {
    wg.Go(func() { run(j) })
}
```

## Opt-out

`// safe-ignore: <reason>` on the `Add` line.

## Limitations

- Only `Add` statements directly in the body of a `go func() { ... }()`
  literal are checked, not calls made through helpers.
