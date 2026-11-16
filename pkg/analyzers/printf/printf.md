# printf

Checks constant format strings of `fmt`, `log` and `testing` printf-style
calls against their arguments.

## What it catches

- a verb without an argument: the output says `%!d(MISSING)`;
- an argument without a verb: the output ends in `%!(EXTRA string=...)`;
- `%w` outside `fmt.Errorf`: `log.Printf` and `t.Errorf` print
  `%!w(...)` instead of the error.

These slip into log lines and error messages that no test reads.

## Bad

```go
log.Printf("sync %s failed after %d tries", id)
fmt.Sprintf("user %s", id, name)
log.Printf("save: %w", err)
```

## Good

```go
log.Printf("sync %s failed after %d tries", id, tries)
fmt.Sprintf("user %s (%s)", id, name)
log.Printf("save: %v", err)
```

## Opt-out

`// safe-ignore: <reason>` on the call line.

## Limitations

- Checked functions: `fmt.Printf`, `Sprintf`, `Errorf`, `Fprintf`, `Appendf`;
  `log.Printf`, `Fatalf`, `Panicf` and the same `*log.Logger` methods;
  `Errorf`, `Fatalf`, `Logf`, `Skipf` on `testing.T`, `B`, `F`, `TB`.
- Only constant formats are checked. Formats with explicit indexes
  (`%[1]s`) and calls that spread a slice (`args...`) are skipped.
- Argument types are not checked; go vet does that during `go test`.
