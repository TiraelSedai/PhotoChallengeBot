@/home/tirael/.codex/RTK.md

When writing any new code, definition of done for the task is when post-task-review skill is satisfied.

Do not add one-line helper methods that only wrap a single obvious operation; inline the operation unless the helper carries real domain meaning or removes meaningful duplication.

Struct fields must not be nil after construction. `New*` constructors must check every dependency that becomes a pointer, interface, function, map, slice, channel, or other nil-able struct field, and panic when any such input is nil. Do not guard struct fields for nil inside ordinary methods; the invariant is guaranteed at construction time. Production code must pass every constructor dependency explicitly instead of relying on no-op/stub/default implementations. Tests may replace fields with generated mocks, but should preserve the same non-nil field invariant unless they intentionally test constructor panics.

Interfaces should usually be defined on the caller side and kept unexported. Export an interface only when repeating it in each consumer would not carry useful local meaning, such as a broad shared logging contract; narrow storage/service dependencies should stay package-local even if several packages ask for similarly named methods.

Generated mocks must use Docker-hosted mockery v3 instead of a Go module tool dependency; run `docker run -v "$PWD":/src -w /src vektra/mockery:3` from the repository root. Mockery config lives in `.mockery.yml` and must use `template: matryer`, `filename: "{{ .InterfaceName | snakecase }}_moq_test.go"`, `pkgname: "{{.SrcPackageName}}"`, `structname: "Moq{{ .InterfaceName | firstUpper }}"`, and `template-data.stub-impl: true` for moq-like `--stub` behavior where missing `XFunc` implementations return zero values.

SQLite writes are assumed to be reliable during normal bot operation. Do not add compensating complexity for the case where Telegram side effects succeed but the immediately following SQLite write fails; treat that as an end-of-world scenario such as a Docker restart at the exact wrong moment, not as a normal idempotency/retry contract.
