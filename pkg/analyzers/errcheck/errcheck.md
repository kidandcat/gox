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

- `fmt.Print`/`Println`/`Printf` etc. are exempt — their error is almost always
  irrelevant in practice.
