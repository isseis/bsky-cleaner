# Test Helper File Organization

Test helper files follow a two-tier classification system based on their scope and dependencies.

For assertion style within test bodies (as opposed to helper file placement),
see the "Assertions" guideline in [CLAUDE.md](/CLAUDE.md#testing-strategy):
prefer the `github.com/stretchr/testify/assert` and `github.com/stretchr/testify/require`
subpackages over hand-written `if`/`t.Errorf` checks in new tests.

## Classification A: `testutil/` Subdirectory (Cross-Package Helpers)

**Use for**: Test helpers and mocks used across multiple packages or that only use public APIs

```
<package>/
├── <implementation>.go
├── <implementation>_test.go
└── testutil/
    ├── mocks.go              # Lightweight mocks (no external dependencies)
    ├── testify_mocks.go      # testify-based mocks (for complex scenarios)
    ├── fixtures.go           # Test fixtures and data
    ├── fixtures_test.go      # Tests for fixture implementations
    └── helpers.go            # Test utility functions
```

**File Naming Rules:**
- **`testutil/mocks.go`**: Simple mock implementations without external library dependencies
- **`testutil/testify_mocks.go`**: Advanced mocks using stretchr/testify framework
- **`testutil/fixtures.go`**: Test fixtures and data (not suffixed with `_test.go`)
- **`testutil/fixtures_test.go`**: Unit tests for fixture implementations
- **`testutil/helpers.go`**: Common test utility functions and setup helpers

**Package Naming:**
- Use a domain-prefixed package name within the `testutil/` subdirectory: `package <domain>testutil`
  - Examples: `package atprototestutil`, `package cleanuptestutil`
- Import without an alias: `<module>/internal/<package>/testutil`
- The unique package name eliminates the need for import aliases at call sites, preventing alias drift across the codebase

**Build Tag Requirement:**
- `testutil/` files that are NOT suffixed with `_test.go` (e.g., `mocks.go`, `fixtures.go`, `helpers.go`) MUST include `//go:build test` because:
  1. They are not recognized by Go's `_test.go` file naming convention for automatic test-only compilation
  2. They will be imported by test files in other packages, so they must be explicitly tagged to prevent inclusion in production binaries
  3. Linters like `golangci-lint` with `gosec` require explicit build tags for files that may not be compiled in production

## Classification B: Package-Level Test Helpers

**Use for**: Test helpers that must remain in the same package due to:
- Adding methods to package-internal types
- Using non-exported (private) package APIs
- Avoiding circular dependencies

**Two subcategories based on API scope:**

### B1: Exported helpers (symbols used by other test packages)

```
<package>/
├── <implementation>.go
├── <implementation>_test.go
└── test_helpers.go           # With //go:build test tag
```

**File Naming Rules:**
- **`test_helpers.go`**: Single file with `//go:build test` tag for package-internal test helpers that export symbols
- If multiple helper categories needed: `test_helpers_<category>.go` (e.g., `test_helpers_client.go`)

**Why this pattern:**
- The `//go:build test` tag marks the file to be excluded from production builds
- The regular filename (without `_test.go` suffix) ensures exported symbols are available to test files in other packages
- Example: `internal/atproto/test_helpers.go` exports `StubPassthroughPDSDoer` and `StubDNSTXTLookup`, used by tests in other packages

### B2: Private helpers (symbols used only within the same package)

```
<package>/
├── <implementation>.go
├── <implementation>_test.go
└── test_helpers_test.go      # No build tag needed
```

**File Naming Rules:**
- **`test_helpers_test.go`**: Single file without build tag for package-internal test helpers with only private symbols
- If multiple helper categories needed: `test_helpers_<category>_test.go` (e.g., `test_helpers_group_test.go`)

**Why this pattern:**
- The `_test.go` suffix automatically ensures the file is only compiled during test builds, no build tag needed
- Useful for internal helper types and functions (lowercase) that are not exported
- Example: `internal/notify/test_helpers_test.go` contains `fakeClock` used only by notify's own tests

## Guidelines for New Test Helpers

When adding new test helper code, follow this decision tree:

1. **Does the helper use only public APIs?**
   - Yes → Continue to step 2 (Classification A)
   - No → Continue to step 4 (likely Classification B)

2. **What type of test helper are you creating?** (Classification A - `testutil/` subdirectory)
   - **Mock implementation** → Choose based on complexity:
     - Simple mock (no external dependencies) → `testutil/mocks.go`
     - Complex mock (using testify/mock) → `testutil/testify_mocks.go`
   - **Helper function** (setup, utilities, fixtures) → `testutil/helpers.go`
   - **Mock tests** → `testutil/mocks_test.go`

3. **Is the helper used by tests in other packages?**
   - Yes → Ensure it uses only public APIs, then place in appropriate `testutil/` file (step 2)
   - No → Continue to step 4

4. **Package-internal considerations** (Classification B)
   Place in `test_helpers.go` or `test_helpers_test.go` (see B1 vs B2 above) if the helper:
   - Adds methods to package-internal types
   - Uses non-exported (private) package APIs
   - Would create circular dependencies if placed in `testutil/` subdirectory

   **B1 vs B2 decision:** Does this helper export public (capitalized) symbols used by test files in other packages?
   - Yes → Use `test_helpers.go` with `//go:build test` tag (B1)
   - No → Use `test_helpers_test.go` without build tag (B2)

**Build Tags:**
- **Classification A (`testutil/` non-`_test.go` files)**: MUST include `//go:build test`
  - Files like `mocks.go`, `fixtures.go`, `helpers.go` are imported by test files in other packages and will not be compiled as test-only via filename alone
  - The build tag ensures they are excluded from production binaries
- **Classification B1 (`test_helpers.go` with exported symbols)**: MUST include `//go:build test`
  - Regular filename (without `_test.go`) allows exported symbols to be available to test files in other packages
  - Build tag ensures the file is excluded from production builds
  - Example: `internal/atproto/test_helpers.go` exports `StubPassthroughPDSDoer`, `StubDNSTXTLookup`
- **Classification B2 (`test_helpers_test.go` with private symbols)**: No build tag required
  - The `_test.go` filename suffix automatically ensures these files are only compiled during test builds
  - Use only for helpers with private (lowercase) symbols used exclusively within the same package's tests
  - Example: `internal/notify/test_helpers_test.go` contains private `fakeClock` helper

**Examples:**
- Mock interface implementation → `testutil/mocks.go` or `testutil/testify_mocks.go` (requires `//go:build test`)
- Test setup helper function → `testutil/helpers.go` (requires `//go:build test`)
- Test fixtures and data → `testutil/fixtures.go` (requires `//go:build test`)
- Exported stub/mock used by other test packages → `test_helpers.go` with `//go:build test` tag (B1)
- Private helper used only by this package's tests → `test_helpers_test.go` without build tag (B2)
