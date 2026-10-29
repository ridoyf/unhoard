# Contributing

Thanks for helping. Unhoard moves and deletes files on other people's PCs, so changes that touch removal get a careful review. Everything else is easy to get in.

## Reporting a bug

Open an issue with:

- your Windows version
- whether Unhoard was running as administrator
- what you did, what you expected and what happened
- the log file, if the bug is a crash or a failed action: `%LOCALAPPDATA%\Unhoard\logs\unhoard.log`

Paths in the log can include your user name. Edit them out if you like.

## Adding or fixing a rule

Most contributions are rules: a folder Unhoard should recognise, or a description that is wrong.

Rules are in [`rules/`](rules), one YAML file per category. A rule needs:

- `id`, `match`, `name`, `owner` and `description`
- `safety`: `safe`, `rebuilds`, `delete-via-app` or `never`. If you are not sure, use `never`.
- `tests` with at least one `match` and one `no_match` path, so the rule cannot quietly catch more than it should

Keep descriptions short and plain: what it is, what happens if you remove it, and the right way to do that. Run `go test ./internal/kb/` to check every rule.

## Code changes

Build and run as described in the [README](README.md#build-from-source). Before you open a pull request:

```
gofmt -l .                 # must print nothing
go vet ./...
go test ./...
cd frontend
npm run format
npm run check
```

CI runs the same checks on Windows.

Some ground rules:

- Logic goes in `internal/`, where it can be tested without Wails. The methods in the root `*.go` files only lock, call into `internal/` and convert types.
- Nothing is deleted directly. Removal goes through `internal/quarantine`, and every path is checked by `internal/protect` in the backend.
- Tests use `t.TempDir()` and never touch real system folders or run real cleanup commands.
- Failures shown to users go through `internal/diagnose` with a plain explanation and a next step, not a raw error.
- Windows 10 and 11 only. Please do not add cross-platform layers.
- Ask in an issue before adding a dependency.

Small, focused pull requests are easiest to review.
