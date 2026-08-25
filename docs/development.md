# Development and releases

## Local setup

```bash
go mod download
gofmt -d flexibilling examples/basic
go vet ./...
go test ./...
go test -race ./...
```

Build the docs locally with:

```bash
uvx --with mkdocs-material mkdocs build --strict
uvx --with mkdocs-material mkdocs serve
```

Open `http://127.0.0.1:8000/` while editing.

## Repository layout

```text
flexibilling/       public package and backend interfaces
flexibilling/*test  unit and SQLite integration tests
examples/           runnable generic example
docs/               MkDocs documentation
.github/workflows/  CI, release, and Pages publishing
```

## CI and documentation

Every push runs formatting, vet, tests, and the example. The documentation
workflow builds the MkDocs site with `--strict` and deploys it to GitHub Pages
without creating a documentation branch.

## Module releases

1. Update the module's version tag and changelog.
2. Run the local check set and inspect the example.
3. Create and push a version tag, then publish the matching GitHub Release.
4. The Go module proxy indexes the public tag automatically.

Go does not need a registry token for a public module release.
