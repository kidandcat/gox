# namedargs

Reports arguments that look swapped: their name, or their `/* label */`,
matches a different parameter of the callee.

## What it catches

`transfer(toID, fromID, amount)` against `func transfer(fromID, toID string, amount int64)`
compiles, passes type checking and passes any test that does not swap them on
purpose. The money goes the wrong way. The names at the call site already say
what each value is, so the rule compares them with the parameter names:

- an identifier or field argument (`toID`, `req.ToID`) whose name equals,
  ignoring case, another parameter's name and not its own;
- a `/* name */` label that names another parameter.

It only reports when the value is also assignable to that other parameter.

## Bad

```go
func transfer(fromID, toID string, amount int64) {}

transfer(toID, fromID, amount)        // both arguments reported
transfer(req.ToID, req.FromID, amount) // fields count too
transfer(/* toID */ a, /* fromID */ b, amount)
```

## Good

```go
transfer(fromID, toID, amount)
transfer(src, dst, amount) // names that match no parameter are not judged
```

## Opt-out

When the reversal is intended, add `// safe-ignore: <reason>` on the argument
or call line.

```go
transfer(toID, fromID, amount) // safe-ignore: refund goes back the other way
```

## Limitations

- One-letter names are ignored (`less(j, i)` is usually a deliberate reversal).
- Literals and expressions without a name (`"u-1"`, `f(x)`) are not judged.
- The variadic tail is ignored.
- Earlier versions asked for a `/* name */` label on every argument of a
  same-typed parameter run. That rule is gone: it produced thousands of labels
  that gofmt moved around and caught no swaps. Existing labels stay valid and
  are now checked against the signature.
