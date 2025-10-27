#!/bin/sh

# --- Configuration ---
CONTAINER_NAME="tuya_ipc"
BINARY_PATH="/app/tuya-ipc-terminal"
ENV_FILE="./.env"

# --- 1. Load Environment Variables from .env file ---
# Check if the .env file exists and source it to load variables (PASSWORD, REGION, EMAIL)
if [ -f "$ENV_FILE" ]; then
    echo "Loading configuration from $ENV_FILE..."
    
    # Source the file. Variables like EMAIL, REGION, and PASSWORD will now be defined.
    # We use 'set -a' to export sourced variables to the environment, ensuring they are 
    # available inside the EXPECT block's sub-process, then 'set +a' to turn it off.
    set -a
    . "$ENV_FILE"
    set +a
else
    echo "ERROR: Configuration file $ENV_FILE not found. Script cannot proceed."
    exit 1
fi

# Ensure necessary variables are set before proceeding
if [ -z "$PASSWORD" ] || [ -z "$REGION" ] || [ -z "$EMAIL" ]; then
    echo "ERROR: One or more required variables (PASSWORD, REGION, EMAIL) are missing or empty in $ENV_FILE."
    exit 1
fi

# --- 2. Execute the refresh command non-interactively inside the running container ---
# The variables are now available in the script's environment.
echo "$PASSWORD" | docker exec -i "$CONTAINER_NAME" \
    /usr/bin/expect -c "
    set timeout 30
    # Spawn the tuya command using variables loaded from the .env file
    spawn $BINARY_PATH auth refresh $REGION $EMAIL --password
    
    # Wait for the password prompt
    expect \"Enter password:\"
    
    # Send the password followed by a carriage return (\r)
    send \"$PASSWORD\r\"
    
    # Wait for the process to complete
    expect eof

    # Wait for the spawned process to exit and capture its status
    catch wait result
    
    # Return the exit code of the spawned process (index 3 of the 'wait' result)
    exit [lindex \$result 3]
"

EXIT_CODE=$?

# --- 3. Final Status Check ---
if [ $EXIT_CODE -eq 0 ]; then
    echo "[$(date)] SUCCESS: Token refresh completed."
else
    echo "[$(date)] ERROR: Token refresh failed with exit code $EXIT_CODE."
    # Add logging or notification logic here if the refresh fails
fi
