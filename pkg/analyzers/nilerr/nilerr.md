# nilerr

Reports `return ..., nil` inside `if err != nil { ... }` when the block never
uses `err`.

## What it catches

The function hit an error and reports success. Callers carry on with a zero
value; the failure shows up later somewhere unrelated, or never. Agents write
this when they copy an early-return block and forget to change the last value.

## Bad

```go
user, err := s.repo.Get(ctx, id)
if err != nil {
    return nil, nil
}
```

## Good

```go
user, err := s.repo.Get(ctx, id)
if err != nil {
    return nil, fmt.Errorf("get user %s: %w", id, err)
}

// "not found" is not a failure here, and the block says so:
if err != nil {
    if errors.Is(err, sql.ErrNoRows) {
        return nil, nil
    }
    return nil, err
}
```

## Opt-out

`// safe-ignore: <reason>` on the `return` line.

## Limitations

- Only the condition `err != nil` (or `nil != err`) on a variable of type
  `error` is recognised.
- Any reference to `err` inside the block, including a log line, is taken as
  handling it.
- A block with a comment is taken as a deliberate decision and skipped:
  `return nil // table may already be gone`.
- A return whose other values come from a call (`return errorResult(...),
  nil`) reports the failure in band and is skipped.
