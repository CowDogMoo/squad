Get this codebase to 75% test coverage.

Start by measuring current coverage with 'go test ./... -coverprofile=coverage.out -count=1'.
Analyze gaps with 'go tool cover -func=coverage.out'.
Prioritize packages by impact — most uncovered statements first.
Write tests for each priority package, verify they pass, and iterate.

Target: 75% total coverage.
Use table-driven tests ([]struct + t.Run) for any function with 2+ test cases.
Only create or modify _test.go files. Never edit source code.
TEST FILE NAMING: Strict 1:1 mapping — foo.go gets foo_test.go. Never create
_extra_test.go, _coverage_test.go, or any *_<suffix>_test.go variant.

BUDGET: You have 200 iterations. Use Write (not Edit) for new files.
Cap 20 iterations per package. Wind down early if needed — a partial
report with before/after numbers is better than no report.

COVERAGE MEASUREMENT: Always use 'go tool cover -func=coverage.out | tail -1'
for total coverage. Never parse coverage.out manually with awk or read it raw.
One command, one iteration.
