#!/bin/bash

# This script is included in the Docker, and is executed at startup of the docker, allowing the user to write
# an arbitrary setup script in `autostart.sh` for the image -- things like installing databases, credentials, etc.
#
# Notice ${NOTEBOOKS} is /notebooks in the container, and is assumed to be mounted from the host directory that
# will have the users notebooks.

# Makes sure we are not using $NB_USER's GOPATH.
export GOPATH=/root/go

# Check if autostart.sh exists and is executable.
AUTOSTART="/root/autostart/autostart.sh"
if [[ -x "${AUTOSTART}" ]]; then
  # Check if it's mounted readonly: we chmod to its current permissions. If it fails we assume it is mounted
  # readonly.
  permissions=$(stat -c '%a' "${AUTOSTART}")
  if chmod "${permissions}" "${AUTOSTART}" 2> /dev/null ; then
    echo "${AUTOSTART} doesn't seem to be mounted readonly, NOT EXECUTING IT." 2>&1
  else
    # Run autostart.sh as root
    echo "Running autostart.sh as root..."
    "${AUTOSTART}"
  fi

else
  echo "No ${AUTOSTART} initialization script."
fi

# Run JupyterLab from $NOTEBOOKS as user $NB_USER.
su -l "${NB_USER}" -c "cd \"${NOTEBOOKS}\" ; jupyter lab"
