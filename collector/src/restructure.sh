#!/bin/bash

# Script to restructure the collector/src directory
# This script moves files to subdirectories and updates package declarations and imports

set -e  # Exit on error

cd /Users/dpage/git/ai-workbench/collector/src

echo "=== Restructuring collector/src directory ==="

# Step 1: Move remaining files (probes are already moved)
echo "Step 1: Moving remaining files..."
mv scheduler.go scheduler/
mv dbutils.go utils/db.go

echo "Files moved successfully"

# Step 2: Update package declarations in probes/
echo "Step 2: Updating package declarations in probes/..."
for file in probes/*.go; do
    sed -i '' 's/^package main$/package probes/' "$file"
done

# Step 3: Update package declarations in database/
echo "Step 3: Updating package declarations in database/..."
for file in database/*.go; do
    sed -i '' 's/^package main$/package database/' "$file"
done

# Step 4: Update package declarations in scheduler/
echo "Step 4: Updating package declarations in scheduler/..."
sed -i '' 's/^package main$/package scheduler/' scheduler/scheduler.go

# Step 5: Update package declarations in utils/
echo "Step 5: Updating package declarations in utils/..."
sed -i '' 's/^package main$/package utils/' utils/db.go

# Step 6: Update imports in main package files
echo "Step 6: Updating imports in main package files..."
for file in *.go; do
    # Add imports for new packages at the top of import block
    if ! grep -q '"github.com/pgedge/ai-workbench/collector/src/probes"' "$file" 2>/dev/null; then
        # Add after the first import line
        sed -i '' '/^import ($/a\
    "github.com/pgedge/ai-workbench/collector/src/probes"\
    "github.com/pgedge/ai-workbench/collector/src/database"\
    "github.com/pgedge/ai-workbench/collector/src/scheduler"\
    "github.com/pgedge/ai-workbench/collector/src/utils"\
' "$file" 2>/dev/null || true
    fi
done

# Step 7: Update type references in main package
echo "Step 7: Updating type references..."
for file in *.go; do
    # Update probe references
    sed -i '' 's/\bMetricsProbe\b/probes.MetricsProbe/g' "$file"
    sed -i '' 's/\bProbeConfig\b/probes.ProbeConfig/g' "$file"
    sed -i '' 's/\bBaseMetricsProbe\b/probes.BaseMetricsProbe/g' "$file"

    # Update database references
    sed -i '' 's/\bSchemaManager\b/database.SchemaManager/g' "$file"
    sed -i '' 's/\bDatastorePool\b/database.DatastorePool/g' "$file"
    sed -i '' 's/\bMonitoredConnectionPoolManager\b/database.MonitoredConnectionPoolManager/g' "$file"

    # Update scheduler references
    sed -i '' 's/\bProbeScheduler\b/scheduler.ProbeScheduler/g' "$file"

    # Update utils references
    sed -i '' 's/\bscanRowsToMaps\b/utils.ScanRowsToMaps/g' "$file"
    sed -i '' 's/\bformatDatabaseInfo\b/utils.FormatDatabaseInfo/g' "$file"
done

# Step 8: Update imports within moved files
echo "Step 8: Updating imports in probes/..."
for file in probes/*.go; do
    # Add import for parent main package if needed
    if grep -q 'MonitoredConnection\|DatastoreConfig' "$file"; then
        sed -i '' '/^import ($/a\
    "github.com/pgedge/ai-workbench/collector/src"\
' "$file"
    fi
done

echo "Step 9: Updating imports in database/..."
for file in database/*.go; do
    if [ "$(basename "$file")" != "schema_test.go" ]; then
        # Add import for parent main package if needed
        if grep -q 'MonitoredConnection\|ApplicationName' "$file"; then
            sed -i '' '/^import ($/a\
    main "github.com/pgedge/ai-workbench/collector/src"\
' "$file"
        fi
    fi
done

echo "Step 10: Updating imports in scheduler/..."
if grep -q 'MonitoredConnection\|DatastorePool' scheduler/scheduler.go; then
    sed -i '' '/^import ($/a\
    "github.com/pgedge/ai-workbench/collector/src/probes"\
    "github.com/pgedge/ai-workbench/collector/src/database"\
    "github.com/pgedge/ai-workbench/collector/src/utils"\
    main "github.com/pgedge/ai-workbench/collector/src"\
' scheduler/scheduler.go
fi

# Step 11: Export utility functions (capitalize first letter)
echo "Step 11: Exporting utility functions..."
sed -i '' 's/func scanRowsToMaps/func ScanRowsToMaps/' utils/db.go
sed -i '' 's/func formatDatabaseInfo/func FormatDatabaseInfo/' utils/db.go

echo ""
echo "=== Restructuring complete! ==="
echo ""
echo "Next steps:"
echo "1. Run 'go mod tidy' to clean up dependencies"
echo "2. Run 'go build' to check for compilation errors"
echo "3. Run 'make test' to verify all tests pass"
echo ""
echo "Note: You will need to manually review and fix any remaining import/reference issues"
echo "that the automated script couldn't handle."
