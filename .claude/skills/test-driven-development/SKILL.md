---
name: test-driven-development
description: >
  Go TDD skill using the RED/GREEN/REFACTOR cycle. Use this skill whenever you are
  writing new functionality, fixing a bug, or refactoring existing Go code in this
  repo. It enforces modular design through unit-first testing and dependency injection.
  Trigger any time the user asks to implement something, add a feature, fix a bug,
  or change behavior — even if they don't say "TDD" or "test". If code is changing,
  this skill should guide the approach.
---

# Test-Driven Development in Go

TDD isn't just a testing strategy — it's a design tool. Writing the test first forces
you to think about the public API, dependencies, and behavior before any implementation
exists. The pain of writing a test is signal: if it's hard to test, the design needs
work, not the test.

Prefer unit tests. Unit tests run fast, give precise failure signals, and reward
modular design. Reach for integration tests only when the behavior genuinely can't be
exercised in isolation.

---

## Test conventions

- Test files live alongside source: `foo.go` → `foo_test.go`
- Use `package foo_test` (external test package) by default. This tests only the public
  API, which prevents tests from coupling to internals and forces a cleaner interface.
  Switch to `package foo` (internal) only when you need to test unexported behavior and
  have a good reason.
- Test functions: `func TestFoo(t *testing.T)`
- Use table-driven tests for multiple cases — they scale cleanly and read like a spec.
- Use `t.Run("description", func(t *testing.T) { ... })` for subtests.
- Use `testify/assert` for non-fatal checks and `testify/require` when a failure should
  stop the test immediately (e.g., if subsequent assertions would panic).

Run tests:
```bash
go test ./...                        # all packages
go test -run TestFoo ./pkg/foo/...   # specific test
go test -v ./...                     # verbose (see subtest names)
go test -race ./...                  # detect data races
```

---

## 🔴 RED — Write a failing test

1. Identify the smallest unit of behavior to add. Name it precisely.
2. Create or open `<source>_test.go` co-located with the source file.
3. Write a test that describes the expected behavior. The production code does not exist
   yet — that's the point. The test should compile (or fail to compile in a way that
   makes sense) and fail for the **right reason**: an assertion failure, not a setup
   error or panic.
4. Run `go test ./...` and confirm the new test fails. If it passes immediately, you've
   either already implemented the behavior or the test isn't testing what you think.

**Example — table-driven:**
```go
func TestAdd(t *testing.T) {
    cases := []struct {
        name     string
        a, b     int
        expected int
    }{
        {"positive", 2, 3, 5},
        {"negative", -1, -2, -3},
        {"zero", 0, 0, 0},
    }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            require.Equal(t, tc.expected, Add(tc.a, tc.b))
        })
    }
}
```

---

## 🟢 GREEN — Write the minimum code to pass

Write only enough production code to make the failing test pass. Resist the urge to
anticipate future requirements — that's premature design. Hardcoding a return value to
pass the first test is fine; the next test case will force you to generalize.

Run `go test ./...` and confirm:
- The new test passes.
- No previously passing tests have regressed.

If tests are hard to make pass because of hidden dependencies (globals, `os.Getenv`,
direct DB calls), that's a design signal. Extract the dependency behind an interface and
inject it. See the **Modular design** section below.

---

## 🔵 REFACTOR — Clean up without breaking

Now that behavior is locked in by tests, improve the code:

- Eliminate duplication
- Improve naming (functions, variables, types)
- Extract helpers or sub-packages if a unit is growing too large
- Apply Go idioms (return early, use named returns sparingly, keep functions short)

Run `go test ./...` after every meaningful change. The tests are your safety net —
trust them.

---

## Repeat

Return to 🔴 RED for the next unit of behavior. Keep each cycle small (one behavior
at a time). Large cycles produce large diffs that are hard to debug when something
breaks.

---

## Modular design

TDD naturally pushes toward modular design, but you have to let it. When you find
yourself writing `os.Getenv(...)`, opening a file, or calling a downstream service
inside a function you're trying to test, stop. That's a dependency that belongs behind
an interface.

**Prefer interfaces for dependencies:**
```go
// Define the interface in the package that needs it (not the package that implements it)
type Store interface {
    Get(ctx context.Context, id string) (*Item, error)
    Put(ctx context.Context, item *Item) error
}

// Accept it as a parameter — easy to swap in tests
type Service struct {
    store Store
}
```

In tests, implement a minimal fake:
```go
type fakeStore struct {
    items map[string]*Item
}

func (f *fakeStore) Get(_ context.Context, id string) (*Item, error) {
    item, ok := f.items[id]
    if !ok {
        return nil, ErrNotFound
    }
    return item, nil
}
```

**Package structure signals:**
- If a package is hard to test, it probably does too much. Split it.
- If two packages are always imported together, consider merging them.
- Circular imports are impossible in Go — if you feel the need, rethink the boundary.

---

## When to use integration tests

Unit tests cover logic. Integration tests cover wiring. Use integration tests when:

- Testing that two real components work together correctly (e.g., a real DB round-trip)
- The behavior depends on I/O that can't be meaningfully faked (e.g., file system edge cases)

Integration tests live in `_integration_test.go` files and are guarded by a build tag:
```go
//go:build integration

package foo_test
```

Run them explicitly:
```bash
go test -tags integration ./...
```

This keeps `go test ./...` fast for the default unit test loop.
