# httptimeout

HTTP clients, servers, and shortcut calls must set an explicit timeout.

## What it catches

`http.Get`, `http.Post`, `http.Head`, `http.PostForm`, and any call on
`http.DefaultClient` go through a zero-Timeout client — a hanging server will
block the goroutine forever. Same trap for a freshly constructed `&http.Client{}`,
`var c http.Client`, or `new(http.Client)` whose `Timeout` is the zero value
(or explicitly set to 0).

`http.DefaultClient.Do(req)` is **not** flagged when `req` is assigned from
`http.NewRequestWithContext` in the same function — the deadline is on the
request.

`http.Server` literals must set both `ReadHeaderTimeout` and `WriteTimeout`
to a non-zero value (Slowloris / hung-write). This is the single most common
cause of "the request never returned" production incidents in LLM-written Go.

## Bad

```go
resp, _ := http.Get(url)

client := &http.Client{Transport: t}
resp, _ := client.Get(url) // client has no Timeout

var c http.Client
_ = new(http.Client)

_ = &http.Server{Addr: ":8080"}
```

## Good

```go
client := &http.Client{Timeout: 30 * time.Second}
resp, err := client.Get(url)

// or
req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
resp, err := http.DefaultClient.Do(req)

_ = &http.Server{
    Addr:              ":8080",
    ReadHeaderTimeout: 10 * time.Second,
    WriteTimeout:      30 * time.Second,
}
```

## Opt-out

`// timeout-ok: <reason>` on the same line as the flagged call, `var`, `new`,
or the opening brace of the literal.

```go
resp, _ := http.Get(url) // timeout-ok: probe via mock RoundTripper in tests

client := &http.Client{ // timeout-ok: streaming server, deadline managed per-request
    Transport: t,
}
```

## Limitations

- Does not track `*http.Client` values or request contexts across function
  boundaries. A helper that returns `*http.Request` built with
  `NewRequestWithContext` will not suppress `DefaultClient.Do` in the caller.
- Does not infer a later `c.Timeout = d` assignment after `var c http.Client`;
  prefer a composite literal, or annotate.
