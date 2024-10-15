#!/bin/bash

# This script is included in the Docker, and is executed at startup of the docker, allowing the user to write
# an arbitrary setup script in `autostart.sh` for the image -- things like installing databases, credentials, etc.
#
# Notice ${NOTEBOOKS} is /notebooks in the container, and is assumed to be mounted from the host directory that
# will have the users notebooks.

# Check if autostart.sh exists
if [[ -f "${NOTEBOOKS}/autostart.sh" ]]; then
  # Check if it's owned by root and is executable
  if [[ "$(stat -c '%U' ${NOTEBOOKS}/autostart.sh)" = "root" && -x "${NOTEBOOKS}/autostart.sh" ]]; then
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


echo "Current directory:"
pwd

# Switch to the $NB_USER, preserving environment variables.
# Notice $PATH gets rewritten anyway, so we need to explicitly set it again with the new user.
su --preserve-environment  $NB_USER -c "export PATH=${PATH} ; jupyter lab"
