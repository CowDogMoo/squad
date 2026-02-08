Analyze this Python codebase's test coverage gaps. Target is 75%.

Measure current coverage with pytest-cov if available.
Identify gaps, classify functions as testable vs needing refactor,
and produce a prioritized report of what to test first.

For each uncovered function, determine:

- Testable: can mock external deps
- Needs refactor: requires source changes (note why)
- Skip: trivial code not worth testing

Include mocking strategy recommendations for each testable function.
Note async functions that need pytest-asyncio.
Read each file ONCE, batch 4-6 Read calls per iteration.
Budget: ≤15 iterations for small codebases, ≤25 for medium/large.

Do NOT write or modify any files.
