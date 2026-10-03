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
default: // exhaustive-ok: any future variant routes here on purpose
}
```

## Opt-out

`// exhaustive-ok: <reason>` on the `default:` line marks intentional fallthrough.

## Limitations

- Integer enums are detected in the package under analysis and in other
  packages of the **same module**. A switch on `model.Kind` from `service/`
  is checked. Switches on standard-library enums (`reflect.Kind`) and on
  enums from third-party modules are not.
- Two constants with the same value (`const Default = Off`) are one variant:
  a case for either name covers both.
- Sealed interfaces are still same-package only. An unexported method cannot
  be implemented in another package.
