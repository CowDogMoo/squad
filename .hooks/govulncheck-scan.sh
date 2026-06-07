#!/bin/bash
set -e

# Check if govulncheck is installed
if ! command -v govulncheck &> /dev/null; then
    echo "govulncheck is not installed. Installing..."
    if ! go install golang.org/x/vuln/cmd/govulncheck@latest; then
        echo "Warning: Failed to install govulncheck, skipping vulnerability scan"
        exit 0
    fi
    echo "govulncheck installed successfully"
fi

# Verify govulncheck is now available
if ! command -v govulncheck &> /dev/null; then
    echo "Warning: govulncheck not found in PATH after installation, skipping scan"
    exit 0
fi

# Vulnerabilities with no fix available (upstream hasn't released a patch).
# Review and remove entries once a fixed version is published.
IGNORED_VULNS=(
    "GO-2026-4887" # github.com/docker/docker - AuthZ plugin bypass (Fixed in: N/A)
    "GO-2026-4883" # github.com/docker/docker - Off-by-one plugin privilege validation (Fixed in: N/A)
)

# Run govulncheck vulnerability scan
echo "Running govulncheck vulnerability scan..."
if ! output=$(govulncheck ./... 2>&1); then
    # Filter out ignored vulnerabilities to check if any actionable ones remain
    filtered_output="$output"
    for vuln in "${IGNORED_VULNS[@]}"; do
        filtered_output=$(echo "$filtered_output" | sed "/Vulnerability.*${vuln}/,/^$/d")
    done

    # Check if filtered output still contains actionable vulnerabilities
    if echo "$filtered_output" | grep -q "Your code is affected by"; then
        # Re-count vulnerabilities after filtering
        remaining=$(echo "$filtered_output" | grep -c "^Vulnerability #" || true)
        if [ "$remaining" -gt 0 ]; then
            echo ""
            echo "❌ govulncheck found vulnerabilities in dependencies!"
            echo "$output"
            echo ""
            echo "Please fix the vulnerabilities before committing."
            echo ""
            echo "To update vulnerable dependencies, run:"
            echo "  go get -u <package>@<fixed-version>"
            echo "  go mod tidy"
            echo ""
            echo "For more information, visit: https://go.dev/security/vuln"
            exit 1
        fi
    fi

    # Only ignored (unfixable) vulnerabilities remain
    echo "⚠️  govulncheck found vulnerabilities with no fix available (ignored):"
    for vuln in "${IGNORED_VULNS[@]}"; do
        if echo "$output" | grep -q "$vuln"; then
            echo "  - $vuln (no upstream fix)"
        fi
    done
    echo "✅ No actionable vulnerabilities found by govulncheck"
    exit 0
fi

echo "✅ No vulnerabilities found by govulncheck"
