# "Front-End to Go Communication" or "Enabling Interactive Widgets"

The `websockets` package serves the javascript (`websocket.js`) that implements an end-to-end message
system between the front-end -- cell outputs in the browser -- to the user's Go code.
The goal is to allow the HTML output in the browser to act not only as a form
of rich output, but also input, interacting with the user's code.

It implements the underlying javascript `WebSocket`, the Jupyter protocol using _custom messages_ (see below), and
on top of that a minimal simpler API in javascript (it can also be used from WASM) to send/receive messages keyed
by an "address key", as well as "synchronized variables", also keyed by an "address key". More details below.

The corresponding kernel side of the protocol is implemented in `gonb/internal/comms`. And the API for exchanging
messages with the front-end for the user is available in `gonb/gonbui/comms`. There one can send/receive messages keyed
by an "address key", as well as "synchronized variables", also keyed by an "address key". More details below.

## Examples for End User

### Go Code Example

This is an example of the typical code that would go in a 

### Front-End Code Example -- Widget implementation


### Front-End With WASM Example


## Relevant Links for Maintainer of the Library

Bits and pieces of information I gathered while researching how to implement this.

1. [Jupyter ZeroMQ messaging protocol](https://jupyter-client.readthedocs.io/en/latest/messaging.html):
   Used to communicate between the _JupyterServer_, the Jupyter WebApp (in the browser) and the 
   Kernel (GoNB).
   a. [Custom Messages](https://jupyter-client.readthedocs.io/en/latest/messaging.html#custom-messages):
      Sub-protocol in the Jupyter's protocol to allow communication from the Front-End to the kernel.
      It doesn't include the part that communicates from Javascript to the JupyterServer (WebSocket),
      see below. Part of the custom messages protocol is defined is a separate section for
      ["comm_info" messages.](https://jupyter-client.readthedocs.io/en/latest/messaging.html#comm-info).
      Notice that the Kernel (GoNB) uses the `Shell` socket, while the front-end uses the `IOPub`
      socket to communicate (through the WebSocket).
2. [JupyterServer Websocket Protocol](https://jupyter-server.readthedocs.io/en/latest/developers/websocket-protocols.html)
   Defines(?) the communication between Javascript and JupyterServer through a WebSocket. 
   It works as a bridge to Jupyter's ZeroMQ messaging system.
   The doc lacks details on the Javascript side: what is the URL of the socket, can there be more than
   one opened at the same time, etc.
   a. `JupyterKernelId`: unique Id created by Jupyter for each kernel execution (at least it reports that 
      in the logs. GoNB captures this Id by extracting it from the filename of the json file passed to
      it when executing (`--kernel=<file.json>` argument). It can be separated from the file name with 
      a regexp like `^.*/kernel-([a-f0-9-]+).json$`. 
   b. Websocket URL to connect (found out by looking at browser tools): 
      `/api/kernels/<kernel_id>/channels`.

