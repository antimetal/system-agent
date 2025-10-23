#!/usr/bin/env bash
# Copyright Antimetal, Inc. All rights reserved.
#
# Use of this source code is governed by a source available license that can be found in the
# LICENSE file or at:
# https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
NC='\033[0m' # No Color

# Check if we're in the right directory
if [[ ! -f "${PROJECT_ROOT}/go.mod" ]]; then
    echo -e "${RED}Error: This script must be run from the system-agent repository root${NC}"
    exit 1
fi

echo -e "${GREEN}Generating eBPF type definitions...${NC}"

# Build the generator tool
echo "Building ebpf-typegen..."
if ! go build -o "${PROJECT_ROOT}/bin/ebpf-typegen" "${PROJECT_ROOT}/tools/ebpf-typegen/main.go"; then
    echo -e "${RED}Failed to build ebpf-typegen${NC}"
    exit 1
fi

# Find all *_types.h files in ebpf/include
HEADER_FILES=$(find "${PROJECT_ROOT}/ebpf/include" -name "*_types.h" -type f)

if [[ -z "$HEADER_FILES" ]]; then
    echo -e "${YELLOW}No *_types.h files found in ebpf/include${NC}"
    exit 0
fi

# Generate Go types for each header file
GENERATED_FILES=()
for header in $HEADER_FILES; do
    basename=$(basename "$header" .h)
    collector_name="${basename%_types}"
    
    output_file="${PROJECT_ROOT}/pkg/performance/collectors/${basename}.go"
    
    echo "Generating $output_file from $header..."
    
    if ! "${PROJECT_ROOT}/bin/ebpf-typegen" \
        -input "$header" \
        -output "$output_file" \
        -package collectors; then
        echo -e "${RED}Failed to generate types for $header${NC}"
        exit 1
    fi
    
    GENERATED_FILES+=("$output_file")
done

# Run go fmt on generated files only
if [ ${#GENERATED_FILES[@]} -gt 0 ]; then
    echo "Formatting generated files..."
    go fmt "${GENERATED_FILES[@]}"
fi

# Generate the gen_types.sh wrapper if it doesn't exist
GEN_SCRIPT="${PROJECT_ROOT}/pkg/performance/collectors/gen_types.sh"
if [[ ! -f "$GEN_SCRIPT" ]]; then
    cat > "$GEN_SCRIPT" << 'EOF'
#!/usr/bin/env bash
# This script regenerates the type definitions from eBPF headers

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/../../.." && pwd)"

# Run the main generation script
exec "${PROJECT_ROOT}/scripts/generate-ebpf-types.sh"
EOF
    chmod +x "$GEN_SCRIPT"
    echo "Created $GEN_SCRIPT"
fi

echo -e "${GREEN}Successfully generated eBPF type definitions${NC}"

# Verify the generated files compile
echo "Verifying generated code compiles..."
if ! (cd "${PROJECT_ROOT}" && go build ./pkg/performance/collectors); then
    echo -e "${RED}Generated code does not compile!${NC}"
    exit 1
fi

echo -e "${GREEN}All done!${NC}"