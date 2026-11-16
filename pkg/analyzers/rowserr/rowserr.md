# rowserr

Reports a `*sql.Rows` loop that never checks `rows.Err()`.

## What it catches

`rows.Next()` returns false at the end of the result set and also when the
connection drops, the context is cancelled or a row fails to decode. Without
`rows.Err()` a partial result is returned as if it were complete. This is the
rowserrcheck linter.

## Bad

```go
rows, err := db.QueryContext(ctx, q)
if err != nil {
    return nil, err
}
defer rows.Close()
for rows.Next() {
    ...
}
return items, nil
```

## Good

```go
for rows.Next() {
    ...
}
if err := rows.Err(); err != nil {
    return nil, err
}
return items, nil
```

## Opt-out

`// safe-ignore: <reason>` on the line that declares `rows`.

## Limitations

- Rows that are returned, passed to a helper or stored are skipped; the rule
  does not follow them into other functions.
- Only variables iterated with `Next` in the same function are checked.
