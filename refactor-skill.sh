#!/bin/bash

# GoRefactor Skill - Intelligent wrapper for refactoring operations
# Usage: ./refactor-skill.sh <command> [options]
#
# Commands:
#   analyze <file>              - Analyze file and show extraction candidates
#   extract-best <file>         - Automatically extract highest-priority candidate
#   extract <file> <line1> <line2> <name>  - Extract specific block
#   simplify <file>             - Apply safe, high-impact extractions
#   plan-diff <diff-file>       - Generate refactoring plan from diff
#   apply-plan <plan-file>      - Execute a refactoring plan

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

GOREFACTOR="${GOREFACTOR:-./gorefactor}"

# Check if gorefactor binary exists
if [ ! -f "$GOREFACTOR" ]; then
    echo -e "${RED}Error: gorefactor binary not found at $GOREFACTOR${NC}"
    echo "Run: go build -o gorefactor main.go"
    exit 1
fi

# Helper function to print section headers
print_header() {
    echo -e "\n${BLUE}=== $1 ===${NC}\n"
}

# Helper function to print success messages
print_success() {
    echo -e "${GREEN}✓ $1${NC}"
}

# Helper function to print warnings
print_warning() {
    echo -e "${YELLOW}⚠ $1${NC}"
}

# Analyze a file and show extraction candidates
analyze() {
    local file=$1

    if [ -z "$file" ]; then
        echo "Usage: $0 analyze <go-file>"
        exit 1
    fi

    if [ ! -f "$file" ]; then
        echo -e "${RED}Error: File not found: $file${NC}"
        exit 1
    fi

    print_header "Analyzing $file"

    # Get recommendations with reasonable defaults
    local recommendations=$($GOREFACTOR recommend "$file" \
        --min-complexity 2 \
        --max-complexity 15 \
        --min-statements 3 \
        --max-statements 40 \
        2>&1)

    if echo "$recommendations" | grep -q "error\|Error"; then
        echo -e "${RED}$recommendations${NC}"
        exit 1
    fi

    # Parse and display recommendations nicely
    echo "$recommendations" | jq -r '
        .blocks[]? |
        "\n📊 Lines \(.startLine)-\(.endLine): \(.complexity) complexity, \(.statementCount) statements",
        "   Variables: \(.readVars | join(", ")) → \(.writeVars | join(", "))",
        "   Extractable: \(.extractable)"
    ' 2>/dev/null || echo "$recommendations"

    # Count recommendations
    local count=$(echo "$recommendations" | jq '[.blocks[]?] | length' 2>/dev/null || echo "unknown")
    print_success "Found $count extraction candidates"
}

# Extract the best candidate from a file
extract_best() {
    local file=$1

    if [ -z "$file" ]; then
        echo "Usage: $0 extract-best <go-file>"
        exit 1
    fi

    if [ ! -f "$file" ]; then
        echo -e "${RED}Error: File not found: $file${NC}"
        exit 1
    fi

    print_header "Finding best extraction candidate in $file"

    # Get recommendations
    local recommendations=$($GOREFACTOR recommend "$file" \
        --min-complexity 2 \
        --max-complexity 15 \
        --min-statements 3 \
        --max-statements 40)

    # Find the best candidate (highest complexity that's extractable)
    local best=$(echo "$recommendations" | jq -r '
        [.blocks[]? | select(.extractable == true)] |
        sort_by(.complexity) | reverse |
        .[0] |
        select(. != null) |
        @json
    ')

    if [ -z "$best" ] || [ "$best" == "null" ]; then
        print_warning "No extractable candidates found"
        exit 0
    fi

    # Parse best candidate details
    local start=$(echo "$best" | jq -r '.startLine')
    local end=$(echo "$best" | jq -r '.endLine')
    local complexity=$(echo "$best" | jq -r '.complexity')
    local statements=$(echo "$best" | jq -r '.statementCount')

    # Generate method name from variables
    local read_vars=$(echo "$best" | jq -r '.readVars[]?' | head -1)
    local write_vars=$(echo "$best" | jq -r '.writeVars[]?' | head -1)
    local method_name="process"

    if [ -n "$read_vars" ] && [ -n "$write_vars" ]; then
        method_name="validate"
    elif [ -n "$write_vars" ]; then
        method_name="calculate"
    elif [ -n "$read_vars" ]; then
        method_name="check"
    fi

    echo "Found candidate at lines $start-$end:"
    echo "  Complexity: $complexity"
    echo "  Statements: $statements"
    echo "  Read vars: $(echo "$best" | jq -r '.readVars | join(", ")')"
    echo "  Write vars: $(echo "$best" | jq -r '.writeVars | join(", ")')"
    echo ""

    # Perform extraction
    print_header "Extracting into method: $method_name"
    $GOREFACTOR extract "$file" "$start" "$end" "$method_name"
    print_success "Extraction complete"
}

# Simplify a file by extracting the best candidates
simplify() {
    local file=$1
    local max_extractions=${2:-3}

    if [ -z "$file" ]; then
        echo "Usage: $0 simplify <go-file> [max-extractions]"
        exit 1
    fi

    if [ ! -f "$file" ]; then
        echo -e "${RED}Error: File not found: $file${NC}"
        exit 1
    fi

    print_header "Simplifying $file (max $max_extractions extractions)"

    local count=0
    while [ $count -lt $max_extractions ]; do
        # Try to find and extract best candidate
        local recommendations=$($GOREFACTOR recommend "$file" \
            --min-complexity 2 \
            --max-complexity 15 \
            --min-statements 3 \
            --max-statements 40)

        # Check if any extractable blocks exist
        if ! echo "$recommendations" | jq -e '[.blocks[]? | select(.extractable == true)] | length > 0' >/dev/null 2>&1; then
            print_warning "No more extractable candidates found"
            break
        fi

        # Find best candidate
        local best=$(echo "$recommendations" | jq -r '
            [.blocks[]? | select(.extractable == true)] |
            sort_by(.complexity) | reverse |
            .[0] |
            select(. != null) |
            @json
        ')

        if [ -z "$best" ] || [ "$best" == "null" ]; then
            break
        fi

        local start=$(echo "$best" | jq -r '.startLine')
        local end=$(echo "$best" | jq -r '.endLine')
        local method_name="extract$((count + 1))"

        echo "Extraction $((count + 1)): lines $start-$end"
        $GOREFACTOR extract "$file" "$start" "$end" "$method_name" > /dev/null

        count=$((count + 1))
    done

    print_success "Completed $count extractions"
}

# Generate a refactoring plan from a diff
plan_diff() {
    local diff_file=$1

    if [ -z "$diff_file" ]; then
        echo "Usage: $0 plan-diff <diff-file>"
        exit 1
    fi

    if [ ! -f "$diff_file" ]; then
        echo -e "${RED}Error: Diff file not found: $diff_file${NC}"
        exit 1
    fi

    print_header "Analyzing diff and generating refactoring plan"

    # Generate plan from diff
    $GOREFACTOR analyze-diff "$diff_file"
}

# Apply a refactoring plan
apply_plan() {
    local plan_file=$1
    local output_file=${2:-}

    if [ -z "$plan_file" ]; then
        echo "Usage: $0 apply-plan <plan-file> [output-file]"
        exit 1
    fi

    if [ ! -f "$plan_file" ]; then
        echo -e "${RED}Error: Plan file not found: $plan_file${NC}"
        exit 1
    fi

    print_header "Applying refactoring plan from $plan_file"

    if [ -n "$output_file" ]; then
        $GOREFACTOR orchestrate "$plan_file" "$output_file"
        print_success "Results saved to $output_file"
    else
        $GOREFACTOR orchestrate "$plan_file"
    fi
}

# Main command dispatcher
main() {
    case "$1" in
        analyze)
            analyze "$2"
            ;;
        extract-best)
            extract_best "$2"
            ;;
        extract)
            if [ -z "$2" ] || [ -z "$3" ] || [ -z "$4" ] || [ -z "$5" ]; then
                echo "Usage: $0 extract <file> <line1> <line2> <method-name>"
                exit 1
            fi
            $GOREFACTOR extract "$2" "$3" "$4" "$5"
            ;;
        simplify)
            simplify "$2" "$3"
            ;;
        plan-diff)
            plan_diff "$2"
            ;;
        apply-plan)
            apply_plan "$2" "$3"
            ;;
        *)
            echo "GoRefactor Skill - Intelligent refactoring operations"
            echo ""
            echo "Usage: $0 <command> [options]"
            echo ""
            echo "Commands:"
            echo "  analyze <file>                    - Analyze file and show extraction candidates"
            echo "  extract-best <file>               - Automatically extract highest-priority candidate"
            echo "  extract <file> <l1> <l2> <name>   - Extract specific code block"
            echo "  simplify <file> [max]             - Apply safe, high-impact extractions"
            echo "  plan-diff <diff-file>             - Generate refactoring plan from diff"
            echo "  apply-plan <plan-file> [output]   - Execute a refactoring plan"
            echo ""
            echo "Environment:"
            echo "  GOREFACTOR                        - Path to gorefactor binary (default: ./gorefactor)"
            exit 1
            ;;
    esac
}

main "$@"
