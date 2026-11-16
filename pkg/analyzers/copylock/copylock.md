# copylock

Reports values that contain a lock being copied: `sync.Mutex`, `RWMutex`,
`WaitGroup`, `Once`, `Cond`, the `sync/atomic` types, or any struct that holds
one of them.

## What it catches

A copied mutex is a second, independent mutex. The original and the copy no
longer exclude each other, so the data they guard races. A copy taken while
the original is locked starts locked and deadlocks the first caller. A copied
`WaitGroup` counts nothing. go vet's copylocks reports the same shapes.

Reported copies:

- value receivers and value parameters (`func (s Store) Get()`);
- `range` value variables (`for _, s := range stores`);
- assignments, `var x = y`, return values, call arguments and composite
  literal fields that read an existing value.

## Bad

```go
type Counter struct {
    mu sync.Mutex
    n  int
}

func (c Counter) Inc() { c.mu.Lock(); c.n++; c.mu.Unlock() } // locks a copy

for _, c := range counters { c.Inc() }
```

## Good

```go
func (c *Counter) Inc() { c.mu.Lock(); c.n++; c.mu.Unlock() }

for i := range counters { counters[i].Inc() }
```

## Opt-out

`// safe-ignore: <reason>` on the same line.

## Limitations

- Composite literals, function call results and `new(T)` are fresh values and
  are never reported.
- Copies through generic type parameters are not followed.
