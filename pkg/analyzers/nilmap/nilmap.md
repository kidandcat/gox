# nilmap

Reports writes to a local map declared with `var m map[K]V` and never
initialised.

## What it catches

Reading a nil map works, writing to one panics: `assignment to entry in nil
map`. The code compiles and passes every test that never reaches the write.
This is staticcheck's SA5000.

## Bad

```go
var counts map[string]int
for _, w := range words {
    counts[w]++
}
```

## Good

```go
counts := make(map[string]int)
for _, w := range words {
    counts[w]++
}
```

## Opt-out

`// safe-ignore: <reason>` on the write.

## Limitations

- Only local variables are checked. Struct fields and package-level maps are
  not followed.
- Any assignment to the variable, or taking its address, anywhere in the
  function counts as initialising it, even on another branch.
