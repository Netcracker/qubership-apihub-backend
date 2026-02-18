---
description: Add a unit test for a Go function or service
---

Create a unit test following Go testing conventions and the project's patterns.

## Rules

1. Test file goes next to the source file with `_test.go` suffix.
2. Test function naming: `Test{FunctionName}_{Scenario}(t *testing.T)`.
3. Use table-driven tests when testing multiple input/output combinations.
4. Use `t.Run()` for sub-tests.
5. For service tests that need mocking, create mock implementations of interfaces.

## Pattern

```go
package service

import "testing"

func TestSomething_WhenValid_ReturnsExpected(t *testing.T) {
    // Arrange
    // ...

    // Act
    result, err := sut.SomeMethod(input)

    // Assert
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if result != expected {
        t.Errorf("got %v, want %v", result, expected)
    }
}
```

## Reference

- @qubership-apihub-service/utils/PagingUtils_test.go (test example)
- @qubership-apihub-service/utils/PackageUtils_test.go (test example)
- @qubership-apihub-service/service/ExportService_test.go (service test example)
