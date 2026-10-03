# namedargs

Requires inline `/* paramName */` comments when consecutive args share a type.

## What it catches

`transfer(orderID, userID)` vs `transfer(userID, orderID)` — both compile, both
pass type checking, both pass any test that doesn't specifically swap them.
Wire transfer goes to the wrong account. This is the highest-cost class of
silent bug in unsupervised AI codegen.

## Bad

```go
func transfer(from, to string, amount int64) {}
transfer(orderID, userID, amount) // swap-prone, silent
```

## Good

Put each labelled argument on its own line:

```go
transfer(
	/* from */ srcID,
	/* to */ dstID,
	amount,
)
```

### gofmt-stability (why own-line)

A single-line label is **not** a `gofmt` fixed point. On one line `gofmt`
relocates the block comment so it trails the *previous* argument:

```go
transfer(/* from */ srcID, /* to */ dstID, amount)
// gofmt rewrites the above to:
transfer(/* from */ srcID /* to */, dstID, amount)
```

Now `/* to */` documents `srcID` instead of `dstID` — the opposite of the
truth, and exactly the swap-bug this rule exists to catch. Because editors run
`gofmt`/`goimports` on save, this happens by accident. The rule therefore
**rejects single-line labels** (firing on every argument that shares a line
with the one before it) and accepts only the own-line form, which `gofmt`
leaves untouched. The first argument may stay on the opening-paren line — it
has no predecessor whose label could be relocated.

## Opt-out

`// safe-ignore: <reason>` on the same line. The annotation suppresses every
namedargs violation on that line.

```go
transfer(a, b, amount) // safe-ignore: caller validated argument order via runtime check
```

## Limitations

- Standard-library calls are exempt — their conventions are well-known and
  labeling them adds noise. "Standard library" is `go list`'s `Standard`
  field, so a dotless module (`module myapp`) is user code and is checked.
- Test files are not exempt. A swapped argument in a test is still a swapped
  argument.
- Same-type pairs of structs and named types are flagged; built-in numeric
  types like `int` / `string` / `bool` are the primary target.
