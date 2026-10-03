# errcheck

Reports calls to error-returning functions whose error is dropped.

## What it catches

Discarded errors are the single largest class of silent production bugs in Go.
The classic pattern an LLM produces — `db.Close()` at the end of a function, or
`json.Unmarshal(b, &v)` with no error check — hides a real failure behind exit
code 0.

## Bad

```go
db.Close()
defer f.Close()
go mayFail()
json.Unmarshal(b, &v)
```

## Good

```go
if err := db.Close(); err != nil { return err }
if err := json.Unmarshal(b, &v); err != nil { return err }

defer func() {
    if err := f.Close(); err != nil {
        log.Printf("close: %v", err)
    }
}()
```

## Opt-out

`// safe-ignore: <reason>` on the same line as the call, `defer`, `go`, or
assignment.

```go
_ = db.Close() // safe-ignore: shutdown path — caller already logged primary error
defer f.Close() // safe-ignore: read-only file
```

`defer f.Close()` is **flagged**, not allowlisted. `Close` can fail (flush,
short write, network filesystem). A reason-required annotation or an explicit
handler in a deferred closure is the opt-out.

## Limitations

- A result is an error when its type implements `error` (`Error() string`).
  That includes the builtin `error` and concrete types such as
  `*ValidationError`. An interface whose method is only *named* `Error`
  (`Error() int`) is not an error.
- `fmt.Print`, `Println`, `Printf`, and the `Fprint` / `Fprintf` / `Fprintln`
  family are exempt for every writer, including `*os.File` and
  `http.ResponseWriter`. Dropping those errors is often a real bug; the
  exemption matches the common `fmt.Fprintf(w, ...)` idiom and keeps the bug
  tier from flooding handler code. Handle the error explicitly when it
  matters.
- `bytes.Buffer` and `strings.Builder` writes, and `hash.Hash.Write`, are
  exempt because their documentation says the error is always nil.
