---
description: Add a new error code and message to the exception package
---

Add a new error code to the project's error system.

## Pattern

In `qubership-apihub-service/exception/ErrorCodes.go`, error codes follow this pattern:

```go
const SomeErrorCode = "123"
const SomeErrorCodeMsg = "Description with $param placeholders"
```

## Usage in code

```go
return nil, &exception.CustomError{
    Status:  http.StatusBadRequest,
    Code:    exception.SomeErrorCode,
    Message: exception.SomeErrorCodeMsg,
    Params:  map[string]interface{}{"param": value},
}
```

## Rules

1. Code is a string number — check existing codes and pick the next available number in the relevant range.
2. Message uses `$paramName` placeholders that get resolved from the Params map.
3. Always create both a `Code` constant and a `Msg` constant.
4. Group related error codes together in the file.

## Reference

- @qubership-apihub-service/exception/ErrorCodes.go (all existing error codes)
- @qubership-apihub-service/exception/Errors.go (CustomError struct definition)
