Get this Python codebase to 75% test coverage.

Start by running Glob **/*.py AND checking pytest-cov in parallel.
If pytest-cov available, measure baseline with 'pytest --cov=<pkg> --cov-branch --cov-report=term-missing -q || true'
where <pkg> is the source package discovered from Glob (e.g., app, src). 0% is fine if no tests exist.
Use Glob **/*.py to discover all source files. Filter out __pycache__, .venv, test_*.py.
Prioritize modules by impact — most functions/classes first.
Write tests for each priority module, verify they pass, and iterate.

Target: 75% total coverage.
Use @pytest.mark.parametrize for any function with 2+ test cases.
Only create or modify test_*.py files. Never edit source code.
TEST FILE LOCATION: ALWAYS place tests in tests/ directory. Create tests/ if it
does not exist. Mirror source structure: <pkg>/tasks/foo.py → tests/tasks/test_foo.py.
Discover actual source dir from Glob (could be src/, app/, lib/, or package name).

MOCK STRATEGY:

- Mock HTTP clients (httpx, requests) with unittest.mock.patch
- Mock file I/O by using tmp_path fixture
- Mock time with freezegun or mock time.time()
- Do NOT mock internal classes — test through public API

CONFTEST.PY STRUCTURE (CRITICAL):

- sys.modules stubs MUST be at MODULE LEVEL (top of file), NOT inside fixtures
- Reason: pytest imports conftest.py BEFORE collecting test files
- If stubs are in fixtures, test imports fail with ModuleNotFoundError
- Pattern: sys.modules stubs at top, then pytest import, then fixtures

ASYNC TESTS:

- Use @pytest.mark.asyncio for async functions
- If pytest-asyncio not available, note in report and skip async tests

ITERATION BUDGET (CRITICAL — do NOT exceed):

- Small codebase (≤15 source files): 15 iterations MAX
- Medium (16-30 files): 25 iterations MAX
- Large (30+ files): 35 iterations MAX

WRITE EARLY — CRITICAL FOR REASONING MODELS:
Do NOT read all files before writing. Interleave reading and writing:

- Iter 1: Glob + coverage measurement
- Iter 2: Read 2-3 source files + pyproject.toml
- Iter 3: Write tests for those files (use Write, not Edit)
- Iter 4: Read 2-3 more source files
- Iter 5: Write tests for those
- Repeat until done, then verify + report

Reasoning models (o1, o3, codex) can exhaust output tokens if they think
about too many files before acting. ACT QUICKLY — produce Write calls early.

PYTEST BUDGET: 2 RUNS MAXIMUM (HARD LIMIT)

- Run #1: After writing ALL test files
- Run #2: After fixing ALL failures in ONE iteration
- Running pytest more than twice = FAILURE. Count your pytest runs.

HARD EFFICIENCY REQUIREMENTS:

- INTERLEAVE reads and writes: Read 2-3 → Write multiple files → Read 2-3 → Write
- ALWAYS use Write, NEVER use Edit. Edit is FORBIDDEN for test files.
- Write MULTIPLE test files per iteration using parallel Write calls.
- If pytest fails, fix ALL failing files in ONE iteration (parallel Write).
- STOP after pytest passes — emit report in SAME response.

COVERAGE MEASUREMENT: Use 'pytest --cov=<pkg> --cov-branch --cov-report=term-missing -q || true'.
Branch coverage is more meaningful than line coverage. Parse the TOTAL line.
If pytest-cov not installed, use basic pytest.

MOCKING: Always use autospec=True when patching. Use AsyncMock for async functions.

REPORT REQUIREMENTS (mandatory):

- Coverage Report section with Before/After/Delta
- Modules Tested table
- Tests Written with descriptions
- Skipped Functions with reasons
- Files Touched listing all test_*.py files
- Validation with pytest results
