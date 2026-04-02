# Test Plan: `dynamicToolDirs` Table-Driven Test

## Status: ✅ Completed

The refactored table-driven test has been implemented in `sandbox/paths_test.go`.

## Implementation

```go
func TestDynamicToolDirs_ReturnsAllExpectedDirectories(t *testing.T) {
    // ... tests all 7 directories including:
    // - GOPATH bin
    // - .cargo/bin
    // - .local/share/mise/shims (with miseRoot as ReadDirs)
    // - .local/bin
    // - .bun/bin
    // - .proto/bin
    // - .qlty/bin
}
```

## Verification

All tests pass:
- `TestDynamicToolDirs_ReturnsAllExpectedDirectories/GOPATH_bin`
- `TestDynamicToolDirs_ReturnsAllExpectedDirectories/.cargo/bin`
- `TestDynamicToolDirs_ReturnsAllExpectedDirectories/.local/share/mise/shims`
- `TestDynamicToolDirs_ReturnsAllExpectedDirectories/.local/bin`
- `TestDynamicToolDirs_ReturnsAllExpectedDirectories/.bun/bin`
- `TestDynamicToolDirs_ReturnsAllExpectedDirectories/.proto/bin`
- `TestDynamicToolDirs_ReturnsAllExpectedDirectories/.qlty/bin`

## Benefits Achieved

| Feature | Status |
|---------|--------|
| Includes `.qlty/bin` | ✅ |
| Checks both BinDir AND ReadDirs | ✅ |
| Single source of truth | ✅ |
| Low maintenance (table-driven) | ✅ |
| Verifies mise special case | ✅ |
