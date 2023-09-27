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
    /**
     * debug_log is used to print out debugging information on what is going on in gonb_comm.
     *
     * It is enabled by setting `globalThis.gonb_comm.debug = true`.
     */
    function debug_log(...data) {
        if (globalThis?.gonb_comm?.debug) {
            console.log(...data);
        }
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
        debug: true,  // Set to true to see verbose debugging messages in the console.
        websocket_is_opened: false,
        _kernel_id: "{{.KernelId}}",
        _ws_url: "ws://" + document.location.host + "/api/kernels/{{.KernelId}}/channels",
        _comm_id: null,
        _self_closed: false,

        // Subscriptions:
        _onopen_subscribers: [],
        _onopen_ack: null,
        _address_subscriptions: {},  // map address -> map id(Symbol) -> callback.
        _address_subscriptions_next_id: 0,
        _address_subscriptions_id_to_address: {},  // map id(Symbol) -> address.

        // Synced Variables:
        _address_to_synced_var: {},  // map address -> variable.
    };
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

        if (globalThis.gonb_comm === gonb_comm) {
            delete globalThis.gonb_comm;
        }
    };

    // Set up websocket handlers.

    gonb_comm._websocket.onerror = (event) => {
        debug_log("GoNB communications (gonb_comm) error:\nEvent = "+JSON.stringify(event));
        gonb_comm.close(1000, `closing due to error in communication - ${event.message}`)
    }

    // onmessage sees incoming messages from WebSocket which includes all types of status/execute_result/etc.
    // It filters out messages that are not part of the "custom messages" protocol ("comm_*" messages), and
    // routes the "comm_*" message appropriately.
    gonb_comm._websocket.onmessage =  (event) => {
        if (globalThis.gonb_comm !== gonb_comm) {
            // Reference lost for object, better close it.
            console.error("gonb_comm from previous kernel still hanging, closing it");
            gonb_comm.close(1000, "gonb_comm from previous kernel still hanging, closing it");
        }

        const msg = JSON.parse(event.data);
        // debug_log(`gonb_comm: websocket received "${msg.msg_type}"`);
        if (msg.msg_type === "comm_msg") {
            gonb_comm._on_comm_msg(msg);
        } else if (msg.msg_type.startsWith("comm_")) {
            debug_log(`gonb_comm: websocket received and ignored "${msg.msg_type}"`);
        }
    };

    /** send a value to the given address.
     *
     * Message with value is immediately enqueued. There is no acknowledgement of delivery --
     * in case of issues, it won't report back to the caller -- but errors are logged in the console.
     *
     * @param address A string, by convention organized hierarchically, separated by "/". E.g.: "/hyperparameters/learning_rate".
     * @param value Any pod (plain-old-data) value, or an object. It will be JASON.stringified.
     */
    gonb_comm.send = function(address, value) {
        debug_log(`gonb_comm.send(${address}, ${value})`);
        this._is_connected.
            then(() => {
                let msg = this._build_raw_message("comm_msg");
                msg.content = {
                    comm_id: this._comm_id,
                    data: {
                        address: address,
                        value: value,
                    },
                }
                debug_log(`async gonb_comm.send(${address}, ${value})`);
                let err = this._send(msg);
                if (err) {
                    console.error(`gonb_comm: failed sending data to address "${address}": ${err.message}`);
                }
        })
    }

    /** subscribe to receive values sent to the given address.
     *
     * @param address A string, by convention organized hierarchically, separated by "/". E.g.: "/hyperparameters/learning_rate".
     * @param callback is called when a new value is received. The signature is `function(address, value)`.
     * @return Symbol that can be used to unsubscribe.
     */
    gonb_comm.subscribe = function(address, callback) {
        let id = Symbol(`gonb_id_${this._address_subscriptions_next_id}`)
        this._address_subscriptions_next_id++;
        let l = this._address_subscriptions[address];
        if (!l) {
            this._address_subscriptions[address] = {id: callback};
        } else {
            this._address_subscriptions[address][id] = callback;
        }
        this._address_subscriptions_id_to_address[id] = address;
    }

    /** unsubscribe uses the id returned when subscribing to unsubscribe from new messages. */
    gonb_comm.unsubscribe = function(subscription_id) {
        let address = this._address_subscriptions_id_to_address[subscription_id];
        if (!address) {
            // Not (or no longer) subscribed.
            return;
        }
        delete this._address_subscriptions_id_to_address[subscription_id];
        let l = this._address_subscriptions[address];
        if (!l) {
            // No longer subscribed ?
            return;
        }
        delete l[subscription_id];
        if (Object.keys(l).length === 0) {
            // No more subscriptions to this address.
            delete this._address_subscriptions[address];
        }
    }

    /** close closes websocket connection and cleans up.
     * Once websocket closes, gonb_comm will be deleted from the global scope.
     */
    gonb_comm.close = function(code, reason) {
        debug_log(`gonb_comm.close(${code}, ${reason}) called`);
        if (globalThis.gonb_comm === this) {
            delete globalThis.gonb_comm;
        }
        this._comm_id = null;  // Won't recognize or deliver any more comm_msg.
        this._self_closed = true;  // Prevents a second deletion of global gonb_comm, since a new one may be in the process of being created.
        this._websocket.close(code, reason);  // Will trigger clean up on this._websocket.onclose().
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
        let address = data?.address;
        if (!address) {
            console.error(`gonb_comm: received comm_msg without a address \n${JSON.stringify(msg, null, 2)}`);
            return;
        }

        if (address === "#comm_open_ack") {
            debug_log(`gonb_comm: received comm_msg addressed to #comm_open_ack.`);
            if (this._onopen_ack) {
                this._onopen_ack();
            }
            return;
        } else if (address === "#heartbeat/ping") {
            // Internal heartbeat request.
            this.send("#heartbeat/pong", true);
            debug_log(`gonb_comm: replied #heartbeat/ping with /pong`);
            return;
        }

        let subscribers = this._address_subscriptions[address];
        if (!subscribers) {
            console.error(`gonb_comm: comm_msg to address \"${address}\" but no one listening.`);
            return;
        }

        let value = data?.value;
        if (!value) {
            console.error(`gonb_comm: comm_msg to address \"${address}\" but with no value!?.`);
            return;
        }
        debug_log(`gonb_comm: delivered comm_msg to address \"${address}\" to ${Object.keys(subscribers).length} listener(s).`)
        for (const key of Reflect.ownKeys(subscribers)) {
            debug_log(`\t> ${key.toString()}.callback(address, value);`);
            let callback = subscribers[key];
            callback(address, value);
        }
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
                kernel_id: this._kernel_id,
                data: {},
            }
            let err = this._send(msg);
            await this._wait_open_ack();
        } catch (err) {
            console.error(`gonb_comm: failed to connect to kernel, communication (and widgets) will not work: ${err.message}`);
            gonb_comm.close(1000, err);
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


    /** newSyncedVariable creates a SyncedVariable object associated to the given address
     *  and initializes its value.
     *
     * If a variable created to that address already exists, it has its value updated and is
     * returned instead.
     *
     * @param address that the SyncedVariable will be bound. Updates are received/sent from/to GoNB keyed
     *        by this address.
     * @param value initial value. If the SyncedVariable already exists, it's udpated to this value.
     * @return SyncedVariable
     */
    gonb_comm.newSyncedVariable = function(address, value) {
        let v = this._address_to_synced_var[address];
        if (v) {
            v.set(value);
            return v;
        }
        v = new SyncedVariable(address, value);
        return v
    }

    /**
     * SyncedVariable constructor.
     * This constructor is hidden inside the anonymous function.
     * Instead, users should use the method `globalThis.gonb_comm.newSyncedVariable`.
     *
     * @param gonb_comm connection to GoNB.
     * @param address address to subscribe to listen and send updates from/to GoNB.
     * @param value initial value of variable.
     */
    function SyncedVariable(address, value) {
        debug_log(`new SyncedVariable(${address}, ${value});`);
        this._address = address;
        this._subscribers = {};  // map symbol -> callback.
        this._next_subscriber_id = 0;
        this._value = null;

        // Update value without sync to GoNB (if the update came from GoNB)
        this._set_no_sync = function(value) {
            if (this._value === value) {
                debug_log(`SyncedVariable(${this._address})._set_no_sync() called with same value.`);
                return;
            }
            debug_log(`SyncedVariable(${this._address})._set_no_sync(${value})`);
            this._value = value;
            for (const key of Reflect.ownKeys(this._subscribers)) {
                debug_log(`\t> ${key.toString()}.callback(value);`);
                let callback = this._subscribers[key];
                callback(value);
            }
        }

        // Subscribe in gonb_comm to receive updates from address.
        if (globalThis.gonb_comm) {
            this._gonb_subscription = gonb_comm.subscribe(address, (address, value) => {
                debug_log(`SyncedVariable(${this._address}) <- ${value} (from GoNB)`)
                this._set_no_sync(value);
            })
        } else {
            console.error(`SyncedVariable(${this._address}) cannot connect to GoNB, globalthis.gonb_comm not defined!? Widgets may not work correctly.`);
        }

        /** set updates the value of the SyncedVariable and,
         * if the value is different from current, sends updates to subscribers and GoNB.
         *
         * @param value Updated value.
         */
        this.set = function (value) {
            if (value === this._value) {
                return;
            }
            this._set_no_sync(value);  // this._value is set here.
            if (globalThis.gonb_comm) {
                globalThis.gonb_comm.send(this._address, value);
            } else {
                console.error(`SyncedVariable(${this._address}) cannot connect to GoNB, globalthis.gonb_comm not defined!? Widgets may not work correctly.`);
            }
        }

        /** get returns the current value of the SyncedVariable. */
        this.get = function() {
            return this._value;
        }

        /** subscribe to changes in the value of the variable.
         * @param callback will be called when the value is changed, and it has a signature function(value).
         * @return symbol that can be used to unsubscribe.
         */
        this.subscribe = function(callback) {
            let subscription_id = Symbol(`gonb_id_${this._next_subscriber_id}`)
            this._next_subscriber_id++;
            this._subscribers[subscription_id] = callback;
            return subscription_id;
        }

        /** unsubscribe to changes.
         * @param subscription_id returned by subscribe.
         */
        this.unsubscribe = function(subscription_id) {
            delete(this._subscribers[subscription_id]);
        }

        this.set(value);  // This will send update to GoNB, if value not null.
        return this;
    }


    /*
    const element = document.querySelector('#my-element');

const observer = new MutationObserver((mutations) => {
  mutations.forEach((mutation) => {
    if (mutation.type === 'childList' && mutation.removedNodes.includes(element) || mutation.removedNodes.includes(element.parentNode)) {
      // The element or its parent was removed from the DOM.
      // Execute your callback function here.
    }
  });
});

observer.observe(element.parentNode, { childList: true });
     */



    // Start connecting protocol ("comm_open", and a "comm_open_ack" message).
    gonb_comm._is_connected = gonb_comm._connect_to_gonb();
})();