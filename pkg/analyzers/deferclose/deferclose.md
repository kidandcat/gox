# deferclose

Reports `defer x.Close()` placed before the error of the call that produced
`x` is checked.

## What it catches

When the call fails, `x` is usually nil. The deferred `Close` then panics
(`resp.Body` on a nil `*http.Response`) or returns an error nobody sees. This
is staticcheck's SA5001.

## Bad

```go
f, err := os.Open(name)
defer f.Close()
if err != nil {
    return err
}

resp, err := client.Do(req)
defer resp.Body.Close()
```

## Good

```go
f, err := os.Open(name)
if err != nil {
    return err
}
defer f.Close()
```

## Opt-out

`// safe-ignore: <reason>` on the `defer` line.

## Limitations

- Only the statement right after the assignment is checked, which is where
  this mistake appears in practice.
- The error must be the last value of the assignment.
