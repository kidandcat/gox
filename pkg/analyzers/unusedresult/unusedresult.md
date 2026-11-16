# unusedresult

Reports calls to side-effect-free functions whose result is thrown away.

## What it catches

The call does nothing on its own, so the line is either dead or a typo:

- `fmt.Sprintf(...)` where `fmt.Printf` or `msg := fmt.Sprintf(...)` was meant;
- `errors.New("...")` / `fmt.Errorf(...)` without `return`;
- `strings.TrimSpace(s)`, `strings.ToLower(s)`: strings are immutable, `s`
  does not change;
- `context.WithValue(ctx, k, v)`, `context.WithTimeout(...)`: `ctx` does not
  change, and the cancel function leaks.

## Bad

```go
if name == "" {
    errors.New("name required")
}
strings.TrimSpace(input)
```

## Good

```go
if name == "" {
    return errors.New("name required")
}
input = strings.TrimSpace(input)
```

## Opt-out

`// safe-ignore: <reason>` on the same line.

## Limitations

- Only the listed standard library functions are checked: `fmt.Sprint*`,
  `fmt.Errorf`, `fmt.Append*`, `errors.New/Join/Unwrap/Is`, the
  `context.With*` constructors, and package-level `strings` and `bytes`
  functions. Methods and user functions are not inferred.
