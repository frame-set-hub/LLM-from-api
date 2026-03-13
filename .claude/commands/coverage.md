Run tests with coverage analysis and report which packages/functions lack coverage.

```bash
go test ./... -coverprofile=coverage.out -covermode=atomic
go tool cover -func=coverage.out
```

Identify untested areas and suggest the most impactful tests to add. Clean up the coverage file when done.
