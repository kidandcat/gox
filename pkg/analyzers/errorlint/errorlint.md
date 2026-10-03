# errorlint

`errors.Is` / `errors.As` / `%w` instead of `==` / type-assert / `%s` on errors.

## What it catches

Comparing errors with `==` or type-asserting them directly breaks when the error
gets wrapped by a downstream caller. The same code that worked yesterday silently
stops matching after someone wraps an error with `fmt.Errorf("...: %w", err)`.

## Bad

```go
if err == sql.ErrNoRows { ... }
if e, ok := err.(*MyErr); ok { ... }
return fmt.Errorf("read: %s", err) // loses unwrap chain
```

## Good

```go
if errors.Is(err, sql.ErrNoRows) { ... }
var e *MyErr
if errors.As(err, &e) { ... }
return fmt.Errorf("read: %w", err)
```

## Opt-out

`// safe-ignore: <reason>` on the same line.

```go
if err == io.EOF { ... } // safe-ignore: hot loop — io.EOF is never wrapped here
```

## Limitations

- Sentinel comparison against `io.EOF` / `io.ErrUnexpectedEOF` is the most common
  legitimate case — those errors are documented to be returned directly, never
  wrapped.
- Format strings are read with Go's own argument indexing: `%[1]v`, `%*d`,
  and `%.*s` attribute the verb to the operand `fmt` would use. A non-literal
  format string is not checked.
