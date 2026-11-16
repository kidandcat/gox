# exhaustive

Requires switch exhaustiveness over iota enums and sealed interfaces.

## What it catches

When you add a new enum value or interface implementation, every `switch` over
the old set silently falls through. The change compiles, tests pass, and
production takes an unhandled path. Forcing exhaustiveness turns that into a
compile-time fail that breaks the build the moment the new variant lands.

## Bad

```go
type Color int
const (
    Red Color = iota
    Green
    Blue
)
switch c {
case Red:
case Green:
} // Blue silently unhandled
```

## Good

```go
switch c {
case Red:
case Green:
case Blue:
}
// or
switch c {
case Red:
case Green:
default:
    return fmt.Errorf("unsupported color %d", c) // handles the rest on purpose
}
// or, for an empty default
switch c {
case Red:
    paint()
default: // exhaustive-ok: other colors need no work
}
```

## Opt-out

A `default:` with at least one statement counts as handling every other
value. An empty `default:` drops them silently, like no default at all, and
needs `// exhaustive-ok: <reason>` on the `default:` line.

## Limitations

- Integer enums are detected in the package under analysis and in other
  packages of the **same module**. A switch on `model.Kind` from `service/`
  is checked. Switches on standard-library enums (`reflect.Kind`) and on
  enums from third-party modules are not.
- Two constants with the same value (`const Default = Off`) are one variant:
  a case for either name covers both.
- Sealed interfaces are still same-package only. An unexported method cannot
  be implemented in another package.
