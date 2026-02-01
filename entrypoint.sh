#!/bin/sh
set -e

# Load environment from secrets if available
if [ -f "/secrets/.env" ]; then
    echo "Loading environment from /secrets/.env"
    export $(grep -v '^#' /secrets/.env | xargs)
fi

# Execute main binary
exec ./main
