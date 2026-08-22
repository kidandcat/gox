# forcetypeassert

Forbids type assertions without the comma-ok form.

## What it catches

`x := v.(T)` panics at runtime if `v` is not a `T`. `x, _ := v.(T)` does not
panic — it silently yields the zero value of `T`, which is the same class of
bug with a quieter failure mode. The two-value form `x, ok := v.(T)` returns
the zero value and a boolean — caller-controlled. The unchecked forms are
almost never what you want in production code, but LLMs frequently write them
because they are shorter.

## Bad

```go
s := v.(string)
s, _ := v.(string)
client := ctx.Value(key).(*Client)
```

## Good

```go
s, ok := v.(string)
if !ok { return ErrUnexpectedType }

client, ok := ctx.Value(key).(*Client)
if !ok { return ErrMissingClient }
```

## Opt-out

`// safe-ignore: <reason>` on the same line.

```go
s := v.(string) // safe-ignore: switch above already proved v is string
```

## Limitations

- Type switches (`switch v.(type)`) are exempt.
