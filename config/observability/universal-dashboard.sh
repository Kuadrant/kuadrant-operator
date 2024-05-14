#!/bin/bash

# Check if two arguments are provided
if [ "$#" -ne 2 ]; then
    echo "Usage: $0 <file1.json> <file2.json>"
    exit 1
fi

# Assign provided arguments to variables
public_file="$1"
original_file="$2"

# Check if provided files exist
if [ ! -f "$public_file" ]; then
    echo "$public_file does not exist."
    exit 1
fi

if [ ! -f "$original_file" ]; then
    echo "$original_file does not exist."
    exit 1
fi

# Load data from a-public.json
public_data=$(<"$public_file")

# Load data from a.json
original_data=$(<"$original_file")

# Extract __requires field from a-public.json
requires_field=$(echo "$public_data" | jq '.__requires')

# Add __requires field to the outermost bracket of a.json
updated_data=$(echo "$original_data" | jq --argjson requires "$requires_field" '. + { "__requires": $requires }')

# Write the updated data back to a.json
echo "$updated_data" > "$original_file"

# Remove the public file
rm $public_file
