# Shell Completion Testing Guide

## Overview

Shell completion tests are OS-aware and will skip tests that don't apply to the current platform. This ensures CI/CD pipelines run reliably across different operating systems.

## Test Structure

### Platform-Specific Tests

Each shell detector test includes a `skipIfNotUnix` or `skipIfNotWindows` field that controls whether the test runs:

```go
tests := []struct {
    name           string
    content        string
    skipIfNotUnix  bool  // Skip on Windows
    wantConfigured bool
}{
    {
        name:           "bash completion with source",
        content:        "# .bashrc\nsource <(pinner completion bash)",
        skipIfNotUnix:  true,  // Skip this test on Windows
        wantConfigured: true,
    },
}
```

### Test Execution Logic

```go
for _, tt := range tests {
    t.Run(tt.name, func(t *testing.T) {
        // Skip shell tests that don't match the current OS
        if tt.skipIfNotUnix && runtime.GOOS == "windows" {
            t.Skip("Skipping Unix-specific test on Windows")
        }
        
        // ... test logic
    })
}
```

## Shell Detection by Platform

| Shell | Linux | macOS | Windows | Notes |
|-------|-------|-------|---------|-------|
| bash | ✅ | ✅ | ⚠️ | Unix-style tests skip on Windows |
| zsh | ✅ | ✅ | ⚠️ | Unix-style tests skip on Windows |
| fish | ✅ | ✅ | ⚠️ | Unix-style tests skip on Windows |
| pwsh | ❌ | ❌ | ✅ | Windows-specific tests skip on Unix |

## Running Tests

### Run all completion tests
```bash
go test ./pkg/cli -run "Test.*Completion"
```

### Run specific shell tests
```bash
go test ./pkg/cli -run "TestBashCompletionDetector"
go test ./pkg/cli -run "TestZshCompletionDetector"
go test ./pkg/cli -run "TestFishCompletionDetector"
go test ./pkg/cli -run "TestPowerShellCompletionDetector"
```

### Run with verbose output
```bash
go test ./pkg/cli -v -run "Test.*Completion"
```

## CI/CD Integration

### Example GitHub Actions Workflow

```yaml
name: Test Shell Completion

on: [push, pull_request]

jobs:
  test:
    strategy:
      matrix:
        os: [ubuntu-latest, macos-latest, windows-latest]
    runs-on: ${{ matrix.os }}
    steps:
      - uses: actions/checkout@v3
      - uses: actions/setup-go@v4
        with:
          go-version: '1.21'
      - name: Run completion tests
        run: go test ./pkg/cli -v -run "Test.*Completion"
```

### Expected Test Results

**Linux/macOS runners:**
- ✅ TestBashCompletionDetector (4 subtests)
- ✅ TestZshCompletionDetector (3 subtests)
- ✅ TestFishCompletionDetector (3 subtests)
- ⏭️ TestPowerShellCompletionDetector (3 subtests skipped)
- ✅ TestCompletionDetectorFactory
- ✅ TestCheckCompletion (3 subtests)

**Windows runners:**
- ⏭️ TestBashCompletionDetector (4 subtests skipped)
- ⏭️ TestZshCompletionDetector (3 subtests skipped)
- ⏭️ TestFishCompletionDetector (3 subtests skipped)
- ✅ TestPowerShellCompletionDetector (3 subtests)
- ✅ TestCompletionDetectorFactory
- ⏭️ TestCheckCompletion (3 subtests skipped)

## Adding New Shell Tests

When adding tests for a new shell:

1. Add the `skipIfNotUnix` or `skipIfNotWindows` field
2. Set it to `true` if the test is platform-specific
3. Add the skip logic at the start of the test function

```go
func TestNewShellCompletionDetector(t *testing.T) {
    tests := []struct {
        name           string
        content        string
        skipIfNotUnix  bool  // Set to true for Unix-only shells
        wantConfigured bool
    }{
        // ... test cases
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            if tt.skipIfNotUnix && runtime.GOOS == "windows" {
                t.Skip("Skipping Unix-specific test on Windows")
            }
            // ... test logic
        })
    }
}
```

## Troubleshooting

### Test Skipping Unexpectedly

If tests are skipping when they shouldn't:

1. Check the `skipIfNotUnix`/`skipIfNotWindows` field values
2. Verify the test case matches the intended platform
3. Ensure `runtime.GOOS` check is correct

### Tests Failing on CI

If tests fail on CI but pass locally:

1. Check the CI runner's OS
2. Verify temp directory permissions
3. Ensure all required dependencies are installed

### Coverage Issues

To ensure good coverage across platforms:

1. Run tests on all target platforms locally
2. Use CI matrix to test on multiple OS
3. Check test coverage with `-cover` flag:
   ```bash
   go test ./pkg/cli -cover -run "Test.*Completion"
   ```
