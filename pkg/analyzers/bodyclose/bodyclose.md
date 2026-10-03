# bodyclose

Reports `*http.Response` values whose `Body` is never closed.

## What it catches

`http.Get`/`Do` return a response whose `Body` MUST be closed even on errors —
otherwise the underlying connection is held forever, leaking file descriptors
and TIME_WAIT sockets. LLMs frequently forget the `defer resp.Body.Close()` line.

## Bad

```go
resp, _ := client.Do(req)
io.Copy(io.Discard, resp.Body)
// leak: Body never closed
```

## Good

```go
resp, err := client.Do(req)
if err != nil { return err }
defer resp.Body.Close()
io.Copy(io.Discard, resp.Body)
```

## Opt-out

`// safe-ignore: <reason>` on the line of the assignment.

```go
resp, _ := client.Do(req) // safe-ignore: probe — body content discarded by transport
```

## Limitations

- Heuristic, not sound. The analyzer requires `X.Body.Close()` on the same
  variable (the same `types.Object`, so a shadowed `resp` does not close the
  outer one) somewhere in the enclosing function, including inside a nested
  function literal. A response returned as-is to the caller
  (`return resp, err`) is treated as handed off. If the response escapes
  through a struct field, a different variable (`r := resp`), or another
  function call (e.g. `defer closeBody(resp)`), the rule still reports it —
  annotate with `// safe-ignore: <reason>`.
