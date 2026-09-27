# D6. Go conventions are the ones dburl and dbmeta keep

Status: Decided.

The conventions in "Go conventions" in `dbmeta/AGENTS.md` apply here:

- Errors are wrapped with `%w`. A message is lower case, starts with a
  gerund, and names what failed.
- A sentinel error is a constant of a defined string type, as in
  `type Error string` and `const ErrX Error = "x"`. A variable made with
  `errors.New` can be reassigned by any importer, and a constant cannot.
- `ctx` is the first parameter and is never stored in a struct.
- Accept interfaces, return concrete types, and keep an interface to three
  methods or fewer, defined where it is consumed.
- A package name is one short word, and an exported name does not repeat it.
- A receiver is one or two letters, the same on every method.
- Generics replace `interface{}` for a container of one type.

The `go-pedantry` skill holds the longer form (D11).
