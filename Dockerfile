# Jupyter Notebook + GoNB docker
#
# To use it, go to a directory that you want to make available to the Jupyter notebook
# (your home directory, or a directory where to store the notebook files). It will be
# mounted on the `host/` sub-directory in JupyterLab.
#
# To start it:
#
# ```
# docker pull janpfeifer/gonb_jupyter:latest
# docker run -it --rm -p 8888:8888 -v "${PWD}":/notebooks/host janpfeifer/gonb_jupyterlab:latest
# ```
#
# Then copy&paste the URL it outputs in your browser.

#######################################################################################################
# Base image from JupyterLab
#######################################################################################################
ARG BASE_IMAGE=jupyter/base-notebook
ARG BASE_TAG=latest
FROM ${BASE_IMAGE}:${BASE_TAG}

# Update apt and install basic utils
USER root
RUN apt-get update --yes
RUN apt-get install --yes --no-install-recommends wget
RUN apt-get install --yes --no-install-recommends git

#######################################################################################################
# Go and GoNB Libraries
#######################################################################################################
ENV GO_VERSION=1.22.2
ENV GONB_VERSION="v0.10.1"
ENV GOROOT=/usr/local/go
ENV GOPATH=/opt/go
ENV PATH=$PATH:$GOROOT/bin:$GOPATH/bin

# Create Go directory for user -- that will not move if the user home directory is moved.
USER root
RUN mkdir ${GOPATH} && chown ${NB_USER}:users ${GOPATH}

USER root
WORKDIR /usr/local
RUN wget --quiet --output-document=- "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz" | tar -xz \
    && go version

# Install GoNB (https://github.com/janpfeifer/gonb) in the user account
USER $NB_USER
WORKDIR ${HOME}
RUN export GOPROXY=direct && \
    go install "github.com/janpfeifer/gonb@${GONB_VERSION}" && \
    go install golang.org/x/tools/cmd/goimports@latest && \
    go install golang.org/x/tools/gopls@latest && \
    gonb --install

#######################################################################################################
# Prepare directory where Jupyter Lab will run, with the notebooks we want to demo
####################################################################################################### \
USER root
ARG NOTEBOOKS=/notebooks

# Create directory where notebooks will be stored, where Jupyter Lab will run by default.
RUN mkdir ${NOTEBOOKS} && chown ${NB_USER}:users ${NOTEBOOKS}

# Make tutorial available by default, so it can be used, and include the latest
# GoNB version locally.
USER $NB_USER
WORKDIR ${NOTEBOOKS}
COPY --link examples/tutorial.ipynb ${NOTEBOOKS}
RUN git clone 'https://github.com/janpfeifer/gonb.git'

#######################################################################################################
# Finishing touches
#######################################################################################################

# Clean up space used by apt.
USER root
RUN apt-get clean && rm -rf /var/lib/apt/lists/*

# Start-up.
USER $NB_USER
WORKDIR ${NOTEBOOKS}

EXPOSE 8888
ENTRYPOINT ["tini", "-g", "--", "jupyter", "lab"]
