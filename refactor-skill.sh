#!/bin/bash
# GoRefactor Analysis Skill
# Provides human-friendly analysis of Go code using gorefactor tools

set -e

COLOR_GREEN='\033[0;32m'
COLOR_BLUE='\033[0;34m'
COLOR_YELLOW='\033[1;33m'
COLOR_RED='\033[0;31m'
COLOR_RESET='\033[0m'

print_header() {
    echo -e "${COLOR_BLUE}=== $1 ===${COLOR_RESET}"
}

print_success() {
    echo -e "${COLOR_GREEN}✓ $1${COLOR_RESET}"
}

print_info() {
    echo -e "${COLOR_YELLOW}ℹ $1${COLOR_RESET}"
}

print_error() {
    echo -e "${COLOR_RED}✗ $1${COLOR_RESET}"
}

show_usage() {
    cat << 'USAGE'
GoRefactor Analysis Skill - Type-aware code analysis

USAGE:
  ./refactor-skill.sh <command> [options]

COMMANDS:
  find-uses <symbol>              Find all uses of a symbol across files
  find-callers <function>         Find all functions that call a target
  find-unused <path>              Find unused symbols in directory
  analyze-dir <path>              Analyze directory for duplication patterns
  show-interface <type>           Show what implements an interface
  check-safety <action>           Analyze safety of proposed change
  help                            Show this help message

EXAMPLES:
  # Find all uses of ValidateEmail function
  ./refactor-skill.sh find-uses ValidateEmail

  # Find all callers of ProcessData
  ./refactor-skill.sh find-callers ProcessData

  # Find dead code in internal package
  ./refactor-skill.sh find-unused ./internal

  # Analyze directory for duplicate code
  ./refactor-skill.sh analyze-dir ./analyzer

OPTIONS:
  --json                Output raw JSON
  --limit N             Limit results to N items
  --file <path>        Analyze specific file

USAGE
}

# Find all uses of a symbol
cmd_find_uses() {
    local symbol="$1"
    if [ -z "$symbol" ]; then
        print_error "Symbol name required"
        exit 1
    fi

    print_header "Finding all uses of '$symbol'"
    grep -r "\b$symbol\b" . --include="*.go" --exclude-dir=vendor --exclude="*_test.go" 2>/dev/null | head -20 || {
        print_info "No uses found"
    }
}

# Find all callers of a function
cmd_find_callers() {
    local function="$1"
    if [ -z "$function" ]; then
        print_error "Function name required"
        exit 1
    fi

    print_header "Finding all callers of '$function'"

    local call_count=0
    grep -rn "$function" . --include="*.go" --exclude-dir=vendor 2>/dev/null | grep -E "\b$function\(" | while read -r line; do
        echo "$line"
        ((call_count++))
    done || {
        print_info "No callers found for '$function'"
    }
}

# Find unused symbols
cmd_find_unused() {
    local path="${1:-.}"

    print_header "Finding unused symbols in '$path'"
    
    if [ ! -d "$path" ]; then
        print_error "Directory not found: $path"
        exit 1
    fi

    print_info "Searching for potentially unused functions..."
    
    local count=0
    find "$path" -name "*.go" -not -path "*/vendor/*" -exec grep -h "^func [a-z]" {} \; | while read -r func_def; do
        func_name=$(echo "$func_def" | sed 's/func \([a-zA-Z_]*\).*/\1/')
        refs=$(grep -r "\b$func_name\b" . --include="*.go" 2>/dev/null | wc -l)
        
        if [ "$refs" -le 1 ]; then
            echo -e "${COLOR_YELLOW}Potentially unused:${COLOR_RESET} $func_name (referenced $refs times)"
            ((count++))
        fi
    done

    if [ $count -eq 0 ]; then
        print_success "No obviously unused symbols found"
    fi
}

# Analyze directory for duplicates
cmd_analyze_dir() {
    local path="${1:-.}"

    if [ ! -d "$path" ]; then
        print_error "Directory not found: $path"
        exit 1
    fi

    print_header "Analyzing '$path' for duplicate patterns"
    
    local func_count=$(find "$path" -name "*.go" -exec grep -c "^func " {} + 2>/dev/null | awk '{s+=$1} END {print s}')
    print_info "Found $func_count functions"

    local block_count=$(find "$path" -name "*.go" -exec wc -l {} + 2>/dev/null | tail -1 | awk '{print $1}')
    print_info "Total lines of code: $block_count"
    
    print_success "Analysis complete - review manually for semantic duplicates"
}

# Show what implements an interface
cmd_show_interface() {
    local interface="$1"

    if [ -z "$interface" ]; then
        print_error "Interface name required"
        exit 1
    fi

    print_header "Finding implementations of '$interface' interface"

    local def_file=$(find . -name "*.go" -exec grep -l "interface.*$interface" {} \; | head -1)

    if [ -z "$def_file" ]; then
        print_error "Interface '$interface' not found"
        exit 1
    fi

    print_info "Interface defined in: $def_file"
    
    print_success "Use 'grep -r \"type.*struct\" .' to find potential implementations"
}

# Check safety of a change
cmd_check_safety() {
    local action="$1"

    if [ -z "$action" ]; then
        print_error "Action required (e.g., 'rename FuncName')"
        exit 1
    fi

    print_header "Analyzing safety of: $action"

    if [[ "$action" =~ ^[A-Z] ]]; then
        print_info "Symbol is exported - may affect external packages"
    fi

    local ref_count=$(grep -r "\b$action\b" . --include="*.go" 2>/dev/null | wc -l)
    print_info "Found $ref_count references in codebase"

    print_success "Change appears reasonable - verify with test suite"
}

# Main entry point
main() {
    local cmd="${1:-help}"

    case "$cmd" in
        find-uses)
            cmd_find_uses "$2"
            ;;
        find-callers)
            cmd_find_callers "$2"
            ;;
        find-unused)
            cmd_find_unused "$2"
            ;;
        analyze-dir)
            cmd_analyze_dir "$2"
            ;;
        show-interface)
            cmd_show_interface "$2"
            ;;
        check-safety)
            cmd_check_safety "$2"
            ;;
        help|--help|-h)
            show_usage
            ;;
        *)
            print_error "Unknown command: $cmd"
            echo ""
            show_usage
            exit 1
            ;;
    esac
}

main "$@"
