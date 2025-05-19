# Jupyter Notebook + GoNB docker
#
# To use it, go to a directory that you want to make available to the Jupyter notebook
# (your home directory, or a directory where to store the notebook files). It will be
# mounted on the `host/` sub-directory in JupyterLab.
#
# To start it:
#
# ```
#   docker pull janpfeifer/gonb_jupyter:latest
#   docker run -it --rm -p 8888:8888 -v "${PWD}":/notebooks/host janpfeifer/gonb_jupyterlab:latest
# ```
#
# Then copy&paste the URL it outputs in your browser.
#
# You can also provide an `autostart.sh` file that is automatically executed at the docker start up --
# it doesn't really need to be shell script. Just **mount it as READONLY in `/root/autostart/autostart.sh**`.
# A typical command line for that would look like:
#
# ```
# docker run -it --rm -p 8888:8888 \
#    -v "${PWD}":/notebooks/host -v "~/work/gonb/autostart:/root/autostart:ro" \
#    janpfeifer/gonb_jupyterlab:latest
# ```

#######################################################################################################
# Base image from JupyterLab
#######################################################################################################
ARG BASE_IMAGE=quay.io/jupyter/base-notebook
ARG BASE_TAG=latest
FROM ${BASE_IMAGE}:${BASE_TAG}

# Update apt and install basic utils that may be helpful for users to install their own dependencies.
USER root
RUN apt update --yes
RUN apt install --yes --no-install-recommends \
    sudo tzdata wget git openssh-client rsync curl

# Give NB_USER sudo power for "/usr/bin/apt-get install/update" or "/usr/bin/apt install/update".
USER root
RUN groupadd apt-users
RUN usermod -aG apt-users $NB_USER
# Allow members of the apt-users group to execute only apt-get commands without a password
RUN echo "%apt-users ALL=(ALL) NOPASSWD: /usr/bin/apt-get update, /usr/bin/apt-get install *" >> /etc/sudoers
RUN echo "%apt-users ALL=(ALL) NOPASSWD: /usr/bin/apt update, /usr/bin/apt install *" >> /etc/sudoers

#######################################################################################################
# Go and GoNB Libraries
#######################################################################################################
ARG GO_VERSION=1.24.3
ENV GOROOT=/usr/local/go
ENV GOPATH=$HOME/go
ENV PATH=$PATH:$GOROOT/bin:$GOPATH/bin

# Add exported variables to $NB_USER .profile: notice we start the docker as root, and it executes
# JupyterLab with a `su -l $NB_USER`, so the environment variables are lost. We could use --preserve_environment
# for GOROOT and GOPATH, but it feels safer to add these to .profile, so the autostart.sh script can also
# execute `su -l $NB_USER -c "<some command>"` if it wants.
USER $NB_USER
WORKDIR ${HOME}
RUN <<EOF
  echo "NB_USER=${NB_USER}" >> .profile
  echo "export PATH=${PATH}" >> .profile
  echo "export GOPATH=${GOPATH}" >> .profile
  echo "export GOROOT=${GOROOT}" >> .profile
EOF

USER root
WORKDIR /usr/local
RUN wget --quiet --output-document=- "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz" | tar -xvz \
    && go version

# Install GoNB (https://github.com/janpfeifer/gonb) in the user account
USER $NB_USER
WORKDIR ${HOME}
RUN go install golang.org/x/tools/cmd/goimports@latest && \
    go install golang.org/x/tools/gopls@latest

# Clone from main, build&install gonb binary, and then install it as a kernel in Jupyter.
# - First introduce the cache-busting argument. This number can be bumped whenever we only want
#   to rebuild the gonb part.
ARG CACHEBUST=2
WORKDIR ${HOME}
RUN git clone 'https://github.com/janpfeifer/gonb.git'
WORKDIR ${HOME}/gonb
RUN go install . && \
    gonb --install

#######################################################################################################
# Prepare directory where Jupyter Lab will run, with the notebooks we want to demo
####################################################################################################### \
USER root
ENV NOTEBOOKS=/notebooks

# Create directory where notebooks will be stored, where Jupyter Lab will run by default.
RUN mkdir ${NOTEBOOKS} ${NOTEBOOKS}/host && chown ${NB_USER}:users ${NOTEBOOKS} ${NOTEBOOKS}/host

# Make tutorial available by default, so it can be used, and include the latest
# GoNB version locally.
USER $NB_USER
WORKDIR ${NOTEBOOKS}
COPY --link ./examples/tutorial.ipynb ${NOTEBOOKS}

#######################################################################################################
# Finishing touches
#######################################################################################################

# Clean up space used by apt.
USER root
RUN apt-get clean && rm -rf /var/lib/apt/lists/*

# Start-up.
USER root
WORKDIR ${NOTEBOOKS}

# Script that checks for `/root/autostart/autostart.sh` (mounted readonly) and then starts JupyterLab.
COPY cmd/check_and_run_autostart.sh /usr/local/bin/

ENTRYPOINT ["tini", "-g", "--", "/usr/local/bin/check_and_run_autostart.sh"]
