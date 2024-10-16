#!/bin/bash

# This script is included in the Docker, and is executed at startup of the docker, allowing the user to write
# an arbitrary setup script in `autostart.sh` for the image -- things like installing databases, credentials, etc.
#
# Notice ${NOTEBOOKS} is /notebooks in the container, and is assumed to be mounted from the host directory that
# will have the users notebooks.

# Makes sure we are not using $NB_USER's GOPATH.
export GOPATH=/root/go

# Check if autostart.sh exists
if [[ -f "${NOTEBOOKS}/autostart.sh" ]]; then
  # Check if it's owned by root and is executable
  if [[ "$(stat -c '%U' "${NOTEBOOKS}/autostart.sh")" = "root" && -x "${NOTEBOOKS}/autostart.sh" ]]; then
    # Run autostart.sh as root
    echo "Running autostart.sh as root..."
    "${NOTEBOOKS}/autostart.sh"
  else
    # Print a message indicating why it's not running
    echo "autostart.sh exists but is not owned by root or not executable. Not running it."
  fi
else
  echo "No autostart.sh initialization script."
fi

# Run JupyterLab from $NOTEBOOKS as user $NB_USER.
su -l "${NB_USER}" -c "cd \"${NOTEBOOKS}\" ; jupyter lab"
