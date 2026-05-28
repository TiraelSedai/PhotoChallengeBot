@/home/tirael/.codex/RTK.md

When writing any new code, definition of done for the task is when post-task-review skill is satisfied.

Do not add one-line helper methods that only wrap a single obvious operation; inline the operation unless the helper carries real domain meaning or removes meaningful duplication.

Struct fields must not be nil after construction. `New*` constructors must check every dependency that becomes a pointer, interface, function, map, slice, channel, or other nil-able struct field, and panic when any such input is nil. Do not guard struct fields for nil inside ordinary methods; the invariant is guaranteed at construction time. Production code must pass every constructor dependency explicitly instead of relying on no-op/stub/default implementations. Tests may replace fields with mocks/fakes, but should preserve the same non-nil field invariant unless they intentionally test constructor panics; generated mocks must use `github.com/vektra/mockery` with the `matryer` template and `template-data.stub-impl: true` for moq-like `--stub` behavior where missing `XFunc` implementations return zero values.

SQLite writes are assumed to be reliable during normal bot operation. Do not add compensating complexity for the case where Telegram side effects succeed but the immediately following SQLite write fails; treat that as an end-of-world scenario such as a Docker restart at the exact wrong moment, not as a normal idempotency/retry contract.
