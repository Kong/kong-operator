#!/usr/bin/env bash
# This script manages telepresence installation, connection, disconnection and uninstallation.
# It's used by the Makefile targets install.telepresence and uninstall.telepresence.

set -e
set -o pipefail

readonly CONFIG="./.config/telepresence/config.yml"

# Function to log messages with different levels.
log() {
  local level="$1"
  local message="$2"
  echo "${level}: ${message}"
}

# Function to handle telepresence installation and connection.
install_telepresence() {
  TELEPRESENCE="$1"
  if [ -z "${TELEPRESENCE}" ]; then
    TELEPRESENCE="telepresence"
    log "WARN" "TELEPRESENCE is not set, falling back to system-wide 'telepresence'"
  else
    log "INFO" "Using TELEPRESENCE=${TELEPRESENCE}"
  fi

  "${TELEPRESENCE}" version

  log "INFO" "Checking telepresence status..."
  if "${TELEPRESENCE}" status 2>/dev/null | grep -q "Status *: *Connected"; then
    log "INFO" "Telepresence is already connected to the cluster"
    exit 0
  else
    log "INFO" "Installing/upgrading telepresence traffic manager"
    OUT=$("${TELEPRESENCE}" --config ${CONFIG} helm install 2>&1 || true)
    
    log "DEBUG" "Install command output: \"$OUT\""
    
    # Check for expected install results.
    if echo "$OUT" | grep -q "Traffic Manager installed successfully"; then
      log "INFO" "Telepresence traffic manager installed successfully"
    # Handle the case when traffic manager is already installed.
    elif echo "$OUT" | grep -q -i "error: traffic-manager version.*is already installed\|already exists\|use 'telepresence helm upgrade' instead to replace it"; then
      log "INFO" "Telepresence traffic manager is already installed"
    # Default case: anything else is an issue.
    else
      log "WARN" "Issue during telepresence installation, will attempt to continue"
      echo "$OUT"
      exit 1
    fi
    
    # Only proceed with upgrade if necessary
    if echo "$OUT" | grep -q "use 'telepresence helm upgrade' instead to replace it"; then
      log "INFO" "Telepresence appears to be already installed, upgrading..."
      OUT=$("${TELEPRESENCE}" --config ${CONFIG} helm upgrade 2>&1 || true)
      
      # Debug output to see exactly what we're getting.
      log "DEBUG" "Upgrade command output: \"$OUT\""
      
      # Check for successful upgrade.
      if echo "$OUT" | grep -q "Traffic Manager upgraded successfully"; then
        log "INFO" "Telepresence traffic manager upgraded successfully"
      # Default case: anything else is an issue.
      else
        log "WARN" "Issue upgrading telepresence traffic manager, will attempt to connect anyway"
        echo "$OUT"
      fi
    fi

    log "INFO" "Connecting to the cluster with telepresence"
    
    # Run the connect command and capture output.
    OUT=$("${TELEPRESENCE}" --config ${CONFIG} connect 2>&1 || true)
    
    log "DEBUG" "Connect command output: \"$OUT\""

    # If successful, output should contain success messages.
    if echo "$OUT" | grep -q "Connected to context\|Launched Daemon"; then
      log "INFO" "Telepresence connected successfully"
    # Check for version mismatch and handle it.
    elif echo "$OUT" | grep -q -i "version mismatch"; then
      log "INFO" "Detected version mismatch during connect, attempting to quit telepresence daemon"
      "${TELEPRESENCE}" --config ${CONFIG} quit -s 2>&1 || true
      log "INFO" "Retrying connection after daemon restart"
      
      # Try connecting again after restarting the daemon.
      OUT=$("${TELEPRESENCE}" --config ${CONFIG} connect 2>&1 || true)
      log "DEBUG" "Connect retry command output: \"$OUT\""
      
      # Check if the retry was successful.
      if echo "$OUT" | grep -q "Connected to context\|Launched Daemon"; then
        log "INFO" "Telepresence connected successfully after retry"
      else
        log "ERROR" "Failed to connect to the cluster with telepresence after retry"
        echo "$OUT"
        exit 3
      fi
    # Default case: there's an error.
    else
      log "ERROR" "Failed to connect to the cluster with telepresence"
      echo "$OUT"
      exit 3
    fi
  fi

  log "INFO" "Telepresence setup complete"
}

# Function to handle telepresence disconnection and uninstallation.
uninstall_telepresence() {
  TELEPRESENCE="$1"
  if [ -z "${TELEPRESENCE}" ]; then
    TELEPRESENCE="telepresence"
    log "WARN" "TELEPRESENCE is not set, falling back to system-wide 'telepresence'"
  else
    log "INFO" "Using TELEPRESENCE=${TELEPRESENCE}"
  fi
  
  log "INFO" "Disconnecting telepresence from cluster and stopping daemon"
  OUT=$("${TELEPRESENCE}" quit -s 2>&1 || true)
  
  # Debug output to see exactly what we're getting.
  log "DEBUG" "Quit command output: \"$OUT\""
  
  # Empty output means success (already disconnected) or "Quit." for first disconnection.
  if [ -z "$OUT" ] || echo "$OUT" | grep -q "^Quit\.$"; then
    log "INFO" "Telepresence disconnected and daemon stopped successfully (or was not connected)"
  else
    log "WARN" "Issue disconnecting telepresence:"
    echo "$OUT"
  fi
  
  log "INFO" "Uninstalling telepresence traffic manager from cluster"
  OUT=$("${TELEPRESENCE}" --config ${CONFIG} helm uninstall 2>&1 || true)
  
  # Debug output to see exactly what we're getting.
  log "DEBUG" "Uninstall command output: \"$OUT\""
  
  # Check for successful uninstall message (which also happens when running multiple times).
  if echo "$OUT" | grep -q "Traffic Manager uninstalled successfully"; then
    log "INFO" "Telepresence traffic manager uninstalled successfully"
  elif echo "$OUT" | grep -q "no Traffic Manager found\|not found"; then
    log "INFO" "No telepresence traffic manager was found to uninstall"
  else
    log "WARN" "Issue uninstalling telepresence traffic manager:"
    echo "$OUT"
  fi
  
  log "INFO" "Telepresence teardown complete"
}

# Main script logic.
if [ "$#" -lt 1 ]; then
  log "ERROR" "Usage: $0 [install|uninstall] [telepresence_path]"
  exit 1
fi

ACTION="$1"
TELEPRESENCE_PATH="${2:-}"

case "$ACTION" in
  install)
    log "INFO" "Setting up telepresence traffic manager in the cluster"
    install_telepresence "${TELEPRESENCE_PATH}"
    ;;
  uninstall)
    log "INFO" "Tearing down telepresence connection and traffic manager"
    uninstall_telepresence "${TELEPRESENCE_PATH}"
    ;;
  *)
    log "ERROR" "Unknown action: $ACTION. Use 'install' or 'uninstall'"
    exit 1
    ;;
esac

exit 0
