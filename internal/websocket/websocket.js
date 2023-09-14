"use strict";

/**
 * Creates a WebSocket connecting to the JupyterServer and through it to the GoNB kernel.
 * It provides a communication API in the global `gonb_comm` object.
 *
 * The `gonb_comm` is destroyed if/when the connection is closed.
 * If another `gonb_comm` already exists, it will do nothing, and assume the previous one
 * is still working.
 *
 * By the end of this script, `gonb_comm` is not yet in a connected state, but it immediately
 * accepts subscriptions to addresses and one can schedule `SendMessage` --> a promise is returned
 * and can be waited on until connection is established and the message is sent.
 *
 * See documentation and examples of how to use in gonb/internal/websockets/README.md.
 */
(() => {
    /** is_debug_enabled indicates whether to debug_log information on what is going on in gonb_comm. */
    function is_debug_enabled() {
        return true;
    }

    /**
     * debug_log is used to print out debugging information on what is going on in gonb_comm.
     *
     * It is enabled by `is_debug_enabled()`.
     */
    function debug_log(...data) {
        if (!is_debug_enabled()) {
            return;
        }
        console.log(...data);
    }

    if (globalThis.gonb_comm) {
        // Already defined.
        console.error("gonb_comm already running: we assume this is after a kernel restart, closing previous instance.");
        globalThis.gonb_comm.close(1000, "kernel restart")
    }
    if (!globalThis["WebSocket"]) {
        // No support for WebSocket.
        console.error("No WebSocket API, no installing gonb_comm.");
        return;
    }
    debug_log("Installing gonb_comm ...");

    // Create gonb_comm, the communication object to GoNB.
    // The `comm` abbreviation comes from Jupyter `comm` protocol used by it.
    let gonb_comm = {
        websocket_is_opened: false,
        _kernel_id: "{{.KernelId}}",
        _ws_url: "ws://" + document.location.host + "/api/kernels/{{.KernelId}}/channels",
        _comm_id: null,

        // Subscriptions:
        _onopen_subscribers: [],
        _onopen_ack: null,
        _self_closed: false,
    }
    globalThis.gonb_comm = gonb_comm; // Make it globally available.
    gonb_comm._websocket = new WebSocket(gonb_comm._ws_url);

    /**
     * Handles opening: mark as ready for business.
     * @param event
     */
    gonb_comm._websocket.onopen = (event) => {
        gonb_comm.websocket_is_opened = true
        debug_log("GoNB communications (gonb_comm) opened:\nOpenEvent = "+JSON.stringify(event));
        // Call onopen subscribers.
        if (gonb_comm._onopen_subscribers) {
            for (let sub of gonb_comm._onopen_subscribers) {
                sub(null);
            }
            gonb_comm._onopen_subscribers = [];
        }
    }

    /**
     * Handles closing of WebSocket connection.
     * @param event
     */
    gonb_comm._websocket.onclose = (event) => {
        debug_log("GoNB communications (gonb_comm) closed:\nCloseEvent = "+JSON.stringify(event));
        if (gonb_comm._onopen_subscribers) {
            // Inform subscribers of open.
            for (let sub of gonb_comm._onopen_subscribers) {
                sub(Error("connection closed"));
            }
            gonb_comm._onopen_subscribers = [];
        }

        if (!gonb_comm._self_closed) {
            delete globalThis.gonb_comm;
        }
    };

    // Set up websocket handlers.

    gonb_comm._websocket.onerror = (event) => {
        debug_log("GoNB communications (gonb_comm) error:\nEvent = "+JSON.stringify(event));
        gonb_comm._websocket.close(1000, `closing due to error in communication - ${event.message}`)
    }

    // onmessage sees incoming messages from WebSocket which includes all types of status/execute_result/etc.
    // It filters out messages that are not part of the "custom messages" protocol ("comm_*" messages), and
    // routes the "comm_*" message appropriately.
    gonb_comm._websocket.onmessage =  (event) => {
        const msg = JSON.parse(event.data);
        debug_log(`gonb_comm: websocket received "${msg.msg_type}"`);
        if (msg.msg_type === "comm_msg") {
            gonb_comm._on_comm_msg(msg);
        }
    };

    /** send a value to the given address.
     *
     * Message is immediately enqueued. There is no acknowledgement of delivery -- in case of issues, it won't report
     * back to the caller -- but errors are logged in the console.
     *
     * @param address A string, by convention organized hierarchically, separated by "/". E.g.: "/hyperparameters/learning_rate".
     * @param value Any pod (plain-old-data) value, or an object. It will be JASON.stringified.
     */
    gonb_comm.send = function(address, value) {
        gonb_comm._is_connected.
            then(() => {
                let msg = this._build_raw_message("comm_msg");
                msg.content = {
                    comm_id: this._comm_id,
                    data: {
                        address: address,
                        value: value,
                    },
                }
                let err = this._send(msg);
                if (err) {
                    console.error(`gonb_comm: failed sending data to address "${address}": ${err.message}`);
                }
        })
    }

    /** close closes websocket connection and cleans up.
     * Once websocket closes, gonb_comm will be deleted from the global scope.
     */
    gonb_comm.close = function(code, reason) {
        this._comm_id = null;  // Won't recognize or deliver any more comm_msg.
        this._self_closed = true;  // Prevents a second deletion of global gonb_comm, since a new one may be in the process of being created.
        this._websocket.close(code, reason);  // Will trigger clean up on this._websocket.onclose().
        delete globalThis.gonb_comm;
    }

    // _on_comm_msg handles "comm_msg"
    gonb_comm._on_comm_msg = function(msg) {
        if (this._comm_id === null) {
            debug_log(`gonb_comm: discarding comm_msg, since comm_id not defined yet.`);
            return;
        }
        if (msg?.content?.comm_id !== this._comm_id) {
            debug_log(`gonb_comm: discarding comm_msg with unknown comm_id "${msg?.content?.comm_id}".`);
            return;
        }
        let data = msg?.content?.data;
        if (data?.comm_open_ack) {
            debug_log(`gonb_comm: received comm_msg with comm_open_ack.`);
            if (this._onopen_ack) {
                this._onopen_ack();
            }
            return;
        }
        debug_log(`gonb_comm: received comm_msg\n${JSON.stringify(msg, null, 2)}`);

        let address = data?.address;
        if (!address) {
            console.error(`gonb_comm: received comm_msg without a address \n${JSON.stringify(msg, null, 2)}`);
            return;
        }

        if (address === "#heartbeat/ping") {
            // Internal heartbeat request.
            this.send("#heartbeat/pong", true);
            return;
        }

        debug_log(`gonb_comm: received comm_msg to address \"${address}\"`)
    }

    /**
     * send is JSON.stringify the message and sends it to the websocket.
     *
     * See _build_raw_message to build compatible messages.
     *
     * @param msg message sent to be sent to the JupyterServer.
     * @returns Error or null.
     */
    gonb_comm._send = function(msg) {
        debug_log(`gonb_comm._send(${this._kernel_id})`);
        let msg_str = JSON.stringify(msg);
        try {
            this._websocket.send(msg_str);
            debug_log(`\tgonb_comm._send() enqueued.`);
            return null;
        } catch (err) {
            debug_log(`gonb_comm._send(${msg_str}) failed: ${err.message}`);
            return err;
        }
    }

    /**
     * _build_raw_message of the given type, with a newly created msg_id.
     * The message has channel set to "shell" -- usual for communicating, and the content is empty.
     *
     * @param msg_type Use both in the header and in the root of the message.
     * @private
     */
    gonb_comm._build_raw_message = function(msg_type) {
        let msg_id = crypto.randomUUID();
        return {
            header: {
                msg_id: msg_id,
                msg_type: msg_type,
            },
            msg_id: msg_id,
            msg_type: msg_type,
            parent_header: {},
            metadata: {},
            content: {},
            channel: "shell",
        };
    }

    gonb_comm._connect_to_gonb = async function() {
        debug_log(`gonb_comm._connect_to_gonb(${this._kernel_id})`);

        try {
            // Create comm_id and send "comm_open".
            await this._wait_websocket();
            this._comm_id = crypto.randomUUID();
            let msg = this._build_raw_message("comm_open");
            msg.content = {
                comm_id: this._comm_id,
                target_name: "gonb_comm",
                data: {},
            }
            let err = this._send(msg);
            await this._wait_open_ack();
        } catch (err) {
            console.error(`gonb_comm: failed to connect to kernel, communication (and widgets) will not work: ${err.message}`);
            gonb_comm.close();
            return Promise.reject(err)
        }
        debug_log(`gonb_comm: operational using comm_id="${this._comm_id}".`);
        return null;
    }

    gonb_comm._wait_websocket = async function() {
        debug_log(`gonb_comm._wait_websocket(${this._kernel_id})`);
        if (this.websocket_is_opened) {
            return true
        }
        return new Promise((resolve, reject) => {
            this._onopen_subscribers.push((err) => {
                if (err) {
                    reject(err);
                } else {
                    resolve(null);
                }
            })
        })
    }

    gonb_comm._wait_open_ack = async function() {
        debug_log(`gonb_comm._wait_open_ack(${this._kernel_id})`);
        return new Promise((resolve, reject) => {
            let timeoutId = setTimeout(
                () => {
                    debug_log("gonb_comm: comm_open_ack not received in time.");
                    reject(Error("comm_open_ack not received in time"));
                },
                100, // milliseconds
            )
            this._onopen_ack = () => {
                clearTimeout(timeoutId);
                resolve(null);
            };
        });
    }

    // Start connecting protocol ("comm_open", and a "comm_open_ack" message).
    gonb_comm._is_connected = gonb_comm._connect_to_gonb();
})();