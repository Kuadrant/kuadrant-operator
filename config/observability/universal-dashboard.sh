#!/bin/bash

# Check if an argument is provided
if [ "$#" -ne 1 ]; then
    echo "Usage: $0 <public_file.json>"
    exit 1
fi

# Assign provided argument to a variable
public_file="$1"

# Check if the provided file exists
if [ ! -f "$public_file" ]; then
    echo "$public_file does not exist."
    exit 1
fi

# Load data from the public file
public_data=$(<"$public_file")

# Extract __inputs.name field and assign to name variable
name=$(echo "$public_data" | jq -r '.__inputs[0].name')
# Encase name variable with ${} so jq can match it.
name=$'${'$name'}'

# Remove the __inputs and __elements fields using jq
updated_data=$(echo "$public_data" | jq 'del(.__inputs, .__elements)')

# Update the uid attribute content to ${datasource}
updated_data=$(echo "$updated_data" | jq --arg old_value $name 'walk(if type == "string" and . == $old_value then "${datasource}" else . end)')

# Write the updated data back to the public file
echo "$updated_data" > $public_file

echo "File $public_file has been updated successfully."
