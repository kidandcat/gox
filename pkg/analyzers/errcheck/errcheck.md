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

## Accepted without annotation

- An explicit `_ =` on cleanup: `_ = f.Close()`, `_ = tx.Rollback()`,
  `_ = os.Remove(tmp)`. Writing the blank already says the error does not
  matter.
- Closing what was only read, in any form: `rows.Close()` on `*sql.Rows`
  (a failed iteration shows up in `rows.Err()`, see `rowserr`) and
  `resp.Body.Close()` on an `*http.Response` or `*http.Request`.
- The calls listed under Limitations, also in the `_ =` form.

## Test files

`*_test.go` files are not analyzed by default. Pass `--tests` (or set
`GOX_TESTS=1`) to include them.

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
- `bytes.Buffer` and `strings.Builder` writes, `hash.Hash.Write` and
  `crypto/rand.Read` are exempt because their documentation says the error is
  always nil (`crypto/rand.Read` since Go 1.24).
