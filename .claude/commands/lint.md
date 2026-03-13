Run Go vet on all packages to catch common issues, then check for any unused imports or formatting problems.

```bash
go vet ./...
gofmt -l .
```

If any issues are found, fix them automatically.
