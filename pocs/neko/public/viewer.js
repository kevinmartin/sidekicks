// node_modules/zustand/esm/vanilla.mjs
var createStoreImpl = (createState) => {
  let state;
  const listeners = /* @__PURE__ */ new Set();
  const setState = (partial, replace) => {
    const nextState = typeof partial === "function" ? partial(state) : partial;
    if (!Object.is(nextState, state)) {
      const previousState = state;
      state = (replace != null ? replace : typeof nextState !== "object" || nextState === null) ? nextState : Object.assign({}, state, nextState);
      listeners.forEach((listener) => listener(state, previousState));
    }
  };
  const getState = () => state;
  const getInitialState = () => initialState2;
  const subscribe = (listener) => {
    listeners.add(listener);
    return () => listeners.delete(listener);
  };
  const api2 = { setState, getState, getInitialState, subscribe };
  const initialState2 = state = createState(setState, getState, api2);
  return api2;
};
var createStore = ((createState) => createState ? createStoreImpl(createState) : createStoreImpl);

// node_modules/zustand/esm/middleware.mjs
var subscribeWithSelectorImpl = (fn) => (set, get, api2) => {
  const origSubscribe = api2.subscribe;
  api2.subscribe = ((selector, optListener, options) => {
    let listener = selector;
    if (optListener) {
      const equalityFn = (options == null ? void 0 : options.equalityFn) || Object.is;
      let currentSlice = selector(api2.getState());
      listener = (state) => {
        const nextSlice = selector(state);
        if (!equalityFn(currentSlice, nextSlice)) {
          const previousSlice = currentSlice;
          optListener(currentSlice = nextSlice, previousSlice);
        }
      };
      if (options == null ? void 0 : options.fireImmediately) {
        optListener(currentSlice, currentSlice);
      }
    }
    return origSubscribe(listener);
  });
  const initialState2 = fn(set, get, api2);
  return initialState2;
};
var subscribeWithSelector = subscribeWithSelectorImpl;

// node_modules/@m1k1o/neko/dist/emitter.js
var Emitter = class {
  handlers = /* @__PURE__ */ new Map();
  on(event, fn) {
    if (!this.handlers.has(event))
      this.handlers.set(event, /* @__PURE__ */ new Set());
    this.handlers.get(event).add(fn);
    return () => this.off(event, fn);
  }
  off(event, fn) {
    this.handlers.get(event)?.delete(fn);
  }
  once(event, fn) {
    const wrap = ((...args) => (this.off(event, wrap), fn(...args)));
    return this.on(event, wrap);
  }
  emit(event, ...args) {
    for (const fn of [...this.handlers.get(event) ?? []])
      fn(...args);
  }
};

// node_modules/@m1k1o/neko/dist/api.js
var ApiError = class extends Error {
  status;
  constructor(status, message) {
    super(message);
    this.status = status;
  }
};
var NekoApi = class {
  url = location.href.replace(/[?#].*$/, "").replace(/\/+$/, "");
  token = "";
  headers(json) {
    const h = {};
    if (json)
      h["Content-Type"] = "application/json";
    if (this.token)
      h["Authorization"] = "Bearer " + this.token;
    return h;
  }
  async req(method, path, body, init) {
    const res = await fetch(this.url + "/api" + path, {
      ...init,
      method,
      credentials: "include",
      headers: this.headers(body !== void 0),
      body: body === void 0 ? void 0 : JSON.stringify(body)
    });
    if (!res.ok) {
      const err = await res.json().catch(() => null);
      throw new ApiError(res.status, err?.message || res.statusText);
    }
    const text = await res.text();
    return text ? JSON.parse(text) : void 0;
  }
  async blob(path) {
    const res = await fetch(this.url + "/api" + path, { credentials: "include", headers: this.headers(false) });
    if (!res.ok)
      throw new ApiError(res.status, res.statusText);
    return res.blob();
  }
  // XHR because fetch cannot report upload progress
  upload(path, form, onProgress) {
    return new Promise((resolve, reject) => {
      const xhr = new XMLHttpRequest();
      xhr.open("POST", this.url + "/api" + path);
      xhr.withCredentials = true;
      for (const [k, v] of Object.entries(this.headers(false)))
        xhr.setRequestHeader(k, v);
      if (onProgress)
        xhr.upload.onprogress = (e) => onProgress({ loaded: e.loaded, total: e.total });
      xhr.onload = () => {
        if (xhr.status < 300)
          return resolve();
        let message = xhr.statusText;
        try {
          message = JSON.parse(xhr.responseText).message || message;
        } catch {
        }
        reject(new ApiError(xhr.status, message || "upload failed"));
      };
      xhr.onerror = () => reject(new ApiError(0, "network error"));
      xhr.send(form);
    });
  }
};

// node_modules/@m1k1o/neko/dist/input/datachannel.js
var OP = {
  // client -> server
  MOVE: 1,
  SCROLL: 2,
  KEY_DOWN: 3,
  KEY_UP: 4,
  BTN_DOWN: 5,
  BTN_UP: 6,
  TOUCH_BEGIN: 8,
  TOUCH_UPDATE: 9,
  TOUCH_END: 10,
  // server -> client
  CURSOR_POSITION: 1,
  CURSOR_IMAGE: 2
};
var TOUCH_OP = { begin: OP.TOUCH_BEGIN, update: OP.TOUCH_UPDATE, end: OP.TOUCH_END };
var DataChannelInput = class {
  dc = null;
  onCursorPosition;
  onCursorImage;
  // the channel the server opened on the peer connection; null once the peer is gone
  bind(dc) {
    if (!dc) {
      this.dc = null;
      return;
    }
    dc.binaryType = "arraybuffer";
    dc.onmessage = (e) => {
      const v = new DataView(e.data);
      const op = v.getUint8(0);
      if (op === OP.CURSOR_POSITION) {
        this.onCursorPosition?.({ x: v.getUint16(3), y: v.getUint16(5) });
      } else if (op === OP.CURSOR_IMAGE) {
        const uri = URL.createObjectURL(new Blob([e.data.slice(11)], { type: "image/png" }));
        this.onCursorImage?.({
          width: v.getUint16(3),
          height: v.getUint16(5),
          x: v.getUint16(7),
          y: v.getUint16(9),
          uri
        });
      }
    };
    this.dc = dc;
  }
  move(x, y) {
    this.send(OP.MOVE, [2, x], [2, y]);
  }
  scroll(deltaX, deltaY, controlKey) {
    this.send(OP.SCROLL, [-2, deltaX], [-2, deltaY], [1, controlKey ? 1 : 0]);
  }
  button(code, down) {
    this.send(down ? OP.BTN_DOWN : OP.BTN_UP, [4, code]);
  }
  key(keysym, down) {
    this.send(down ? OP.KEY_DOWN : OP.KEY_UP, [4, keysym]);
  }
  touch(phase, id, x, y, pressure) {
    this.send(TOUCH_OP[phase], [4, id], [-4, x], [-4, y], [1, pressure]);
  }
  // opcode, u16 payload length, the fields; dropped while the channel is not open
  send(op, ...fields) {
    if (this.dc?.readyState !== "open")
      return;
    const len = fields.reduce((n, [b]) => n + Math.abs(b), 0);
    const v = new DataView(new ArrayBuffer(3 + len));
    v.setUint8(0, op);
    v.setUint16(1, len);
    let o = 3;
    for (const [b, val] of fields) {
      if (b === 1)
        v.setUint8(o, val);
      else if (b === 2)
        v.setUint16(o, val);
      else if (b === -2)
        v.setInt16(o, val);
      else if (b === 4)
        v.setUint32(o, val);
      else
        v.setInt32(o, val);
      o += Math.abs(b);
    }
    this.dc.send(v.buffer);
  }
};

// node_modules/@m1k1o/neko/dist/transport/webrtc.js
var RECONNECT_MAX = 10;
var RECONNECT_BACKOFF_MS = 1500;
var OFFER_TIMEOUT_MS = 8e3;
var WebRTCTransport = class {
  kind = "webrtc";
  input = new DataChannelInput();
  static supported() {
    return typeof RTCPeerConnection !== "undefined";
  }
  events = new Emitter();
  state = {
    status: "disconnected",
    size: { width: 0, height: 0 },
    video: { playable: false, playing: false, volume: 1, muted: false, mutedByAutoplay: false }
  };
  video = null;
  session = null;
  unsubs = [];
  pc = null;
  // the server trickles its candidates before signal/provide; hold them until the offer is applied
  candidates = [];
  failures = 0;
  retryTimer = 0;
  offerTimer = 0;
  constructor() {
    this.input.onCursorPosition = (p) => this.events.emit("cursor.position", p);
    this.input.onCursorImage = (img) => this.events.emit("cursor.image", img);
  }
  get element() {
    return this.video;
  }
  on(event, cb) {
    return this.events.on(event, cb);
  }
  /////////////////////////////
  // signalling + peer
  /////////////////////////////
  // again on every reconnect: whatever a previous session left (its peer, retries, the count)
  // goes, and its last picture stays on the element until the new stream replaces it
  async connect(session2) {
    this.unsubscribe();
    this.closePeer();
    this.failures = 0;
    this.session = session2;
    const on = (event, cb) => this.unsubs.push(session2.on(event, cb));
    on("signal/provide", (p) => this.onProvide(p).catch((err) => console.error("[neko] webrtc setup failed", err)));
    const renegotiate = (p) => this.onOffer(p.sdp).catch((err) => console.error("[neko] webrtc renegotiation failed", err));
    on("signal/offer", renegotiate);
    on("signal/restart", renegotiate);
    on("signal/answer", (p) => this.pc?.setRemoteDescription({ type: "answer", sdp: p.sdp }).catch(() => {
    }));
    on("signal/candidate", (p) => {
      if (this.pc?.remoteDescription)
        this.pc.addIceCandidate(p).catch(() => {
        });
      else
        this.candidates.push(p);
    });
    on("signal/close", () => this.pc && this.onPeerState("failed"));
    this.setStatus("connecting");
    this.requestPeer();
  }
  // the stream is over: the peer, timers, counters and the picture go (a reconnect does not come
  // through here but through connect(), which keeps the picture)
  close() {
    this.unsubscribe();
    this.session = null;
    this.closePeer();
    this.failures = 0;
    if (this.video) {
      this.video.pause();
      this.video.srcObject = null;
    }
    this.state.video.playable = false;
    this.setStatus("disconnected");
  }
  unsubscribe() {
    this.unsubs.splice(0).forEach((off) => off());
  }
  requestPeer() {
    this.candidates = [];
    this.session?.send("signal/request", { video: {}, audio: {} });
    clearTimeout(this.offerTimer);
    this.offerTimer = window.setTimeout(() => this.session && !this.pc && this.setStatus("unavailable"), OFFER_TIMEOUT_MS);
  }
  closePeer() {
    clearTimeout(this.retryTimer);
    clearTimeout(this.offerTimer);
    this.pc?.close();
    this.pc = null;
    this.input.bind(null);
    this.events.emit("cursor.position", null);
  }
  async onProvide({ sdp, iceservers }) {
    this.closePeer();
    const pc = this.pc = new RTCPeerConnection({ iceServers: iceservers ?? [] });
    pc.onicecandidate = (e) => e.candidate && this.session?.send("signal/candidate", e.candidate.toJSON());
    pc.ontrack = (e) => this.onTrack(e);
    pc.ondatachannel = (e) => this.input.bind(e.channel);
    pc.onconnectionstatechange = () => this.pc === pc && this.onPeerState(pc.connectionState);
    pc.onnegotiationneeded = async () => {
      if (pc.signalingState !== "stable" || !pc.remoteDescription)
        return;
      await pc.setLocalDescription(await pc.createOffer());
      this.session?.send("signal/offer", { sdp: pc.localDescription.sdp });
    };
    await this.onOffer(sdp);
  }
  async onOffer(sdp) {
    const pc = this.pc;
    if (!pc)
      return;
    await pc.setRemoteDescription({ type: "offer", sdp });
    for (const c of this.candidates.splice(0))
      pc.addIceCandidate(c).catch(() => {
      });
    await pc.setLocalDescription(await pc.createAnswer());
    if (/^m=video 0 /m.test(pc.localDescription.sdp)) {
      const codec = sdp.match(/^m=video[\s\S]*?a=rtpmap:\d+ ([\w-]+)/m)?.[1] ?? "the stream";
      return this.events.emit("error", new Error(`this browser cannot play ${codec} video`));
    }
    this.session?.send("signal/answer", { sdp: pc.localDescription.sdp });
  }
  onPeerState(s) {
    if (s === "connected") {
      this.failures = 0;
      this.setStatus("connected");
    } else if (s === "disconnected") {
      this.setStatus("connecting");
    } else if (s === "failed") {
      this.setStatus("connecting");
      this.closePeer();
      if (++this.failures > RECONNECT_MAX)
        return this.events.emit("error", new Error("video connection failed (WebRTC)"));
      clearTimeout(this.retryTimer);
      this.retryTimer = window.setTimeout(() => this.requestPeer(), RECONNECT_BACKOFF_MS * Math.min(this.failures, 4));
    }
  }
  onTrack({ track, streams, receiver }) {
    if ("jitterBufferTarget" in receiver)
      receiver.jitterBufferTarget = 0;
    if (track.kind !== "video" || !this.video)
      return;
    this.video.srcObject = streams[0];
    if (this.state.video.playing)
      this.play().catch(() => {
      });
  }
  // share local media (microphone); renegotiation happens via onnegotiationneeded
  shareMedia(stream) {
    const pc = this.pc;
    if (!pc)
      throw new Error("not connected");
    const senders = stream.getTracks().map((track) => pc.addTrack(track, stream));
    return () => {
      for (const s of senders)
        if (this.pc?.getSenders().includes(s))
          this.pc.removeTrack(s);
    };
  }
  /////////////////////////////
  // the element
  /////////////////////////////
  attach(container) {
    this.video?.remove();
    const video = this.video = document.createElement("video");
    video.playsInline = true;
    video.style.cssText = "position:absolute;inset:0;width:100%;height:100%;background:transparent";
    const v = this.state.video;
    const sync = () => this.emitState();
    video.addEventListener("canplaythrough", () => (v.playable = true, sync()));
    video.addEventListener("playing", () => (v.playing = true, sync()));
    video.addEventListener("pause", () => (v.playing = false, sync()));
    video.addEventListener("emptied", () => (v.playable = v.playing = false, sync()));
    video.addEventListener("volumechange", () => (v.muted = video.muted, v.volume = video.volume, sync()));
    video.addEventListener("resize", () => {
      this.state.size = { width: video.videoWidth, height: video.videoHeight };
      sync();
    });
    v.muted = video.muted;
    v.volume = video.volume;
    container.append(video);
    this.emitState();
    return () => {
      video.remove();
      if (this.video === video)
        this.video = null;
    };
  }
  setPlaying(on) {
    if (on)
      return this.play();
    this.video?.pause();
    return Promise.resolve();
  }
  setVolume(volume) {
    if (this.video)
      this.video.volume = volume;
  }
  setMuted(on) {
    if (this.video)
      this.video.muted = on;
    if (!on) {
      this.state.video.mutedByAutoplay = false;
      this.emitState();
    }
  }
  async play() {
    const video = this.video;
    if (!video)
      return;
    try {
      await video.play();
    } catch (err) {
      if (video.muted || err.name !== "NotAllowedError")
        throw err;
      video.muted = true;
      this.state.video.mutedByAutoplay = true;
      this.emitState();
      await video.play();
      document.addEventListener("click", () => this.setMuted(false), { once: true });
    }
  }
  setStatus(status) {
    this.state.status = status;
    this.emitState();
  }
  emitState() {
    const { status, size, video } = this.state;
    this.events.emit("state", { status, size: { ...size }, video: { ...video } });
  }
};

// node_modules/@m1k1o/neko/dist/input/websocket.js
var MOVE_COALESCE_MS = 16;
var WebSocketInput = class {
  socket;
  pending = null;
  moveTimer = 0;
  constructor(socket) {
    this.socket = socket;
  }
  // one move per frame, the latest wins
  move(x, y) {
    this.pending = { x, y };
    if (!this.moveTimer)
      this.moveTimer = window.setTimeout(() => this.flush(), MOVE_COALESCE_MS);
  }
  scroll(deltaX, deltaY, controlKey) {
    this.flush();
    this.socket.send("control/scroll", { delta_x: deltaX, delta_y: deltaY, control_key: controlKey });
  }
  button(code, down) {
    this.flush();
    this.socket.send(down ? "control/buttondown" : "control/buttonup", { code });
  }
  key(keysym, down) {
    this.flush();
    this.socket.send(down ? "control/keydown" : "control/keyup", { keysym });
  }
  touch(phase, id, x, y, pressure) {
    this.flush();
    this.socket.send(`control/touch${phase}`, { touch_id: id, x, y, pressure });
  }
  flush() {
    clearTimeout(this.moveTimer);
    this.moveTimer = 0;
    if (!this.pending)
      return;
    this.socket.send("control/move", this.pending);
    this.pending = null;
  }
};

// node_modules/@m1k1o/neko/dist/client.js
var RECONNECT_MAX2 = 10;
var RECONNECT_BACKOFF_MS2 = 1500;
var STALE_MS = 25e3;
var defaultSettings = () => ({
  private_mode: false,
  locked_logins: false,
  locked_controls: false,
  control_protection: false,
  implicit_hosting: false,
  inactive_cursors: false,
  merciful_reconnect: false
});
var initialState = () => ({
  authenticated: false,
  connection: { url: location.href, status: "disconnected" },
  video: { playable: false, playing: false, volume: 1, muted: false, mutedByAutoplay: false },
  control: {
    host_id: null,
    locked: false,
    clipboard: null,
    scroll: { inverse: true, sensitivity: 0 },
    keyboard: { layout: "us", variant: "" },
    touch: false
  },
  screen: { size: { width: 1280, height: 720, rate: 30 }, configurations: [] },
  session_id: null,
  sessions: {},
  settings: defaultSettings(),
  mobile_keyboard_open: false
});
var selectSession = (s) => s.session_id ? s.sessions[s.session_id] ?? null : null;
var selectControlling = (s) => s.control.host_id !== null && s.control.host_id === s.session_id;
var selectIsAdmin = (s) => !!selectSession(s)?.profile.is_admin;
var NekoClient = class {
  store = createStore()(subscribeWithSelector(initialState));
  api = new NekoApi();
  events = new Emitter();
  transport;
  // where input devices send: the transport's own channel, or the websocket
  input;
  ws = null;
  introduced = false;
  // system/init arrived on the current socket: the transport's reports count
  // every websocket message, for the transport's signalling
  messages = new Emitter();
  wanted = false;
  // user asked to be connected
  attempts = 0;
  reconnectTimer = 0;
  reconnectGen = 0;
  // a close()+connect() while a reconnect was deciding must win
  staleTimer = 0;
  lastMessage = 0;
  resume = false;
  // playback was running when the socket was lost: pick it up on the new stream
  clipboardAt = 0;
  // when the remote clipboard last changed
  localSeen = { text: "", at: 0 };
  // the local clipboard text last handed over, and when
  opts;
  constructor(opts = {}) {
    this.opts = opts;
    const t = opts.transport;
    this.transport = typeof t === "function" ? t() : t ?? new WebRTCTransport();
    this.input = this.transport.input ?? new WebSocketInput(this);
    this.transport.on("state", (s) => this.onTransportState(s));
    this.transport.on("error", (err) => this.close(err));
    const { store } = this;
    store.subscribe(selectControlling, () => this.sendKeyboardMap());
    store.subscribe((s) => s.control.keyboard.layout + "/" + s.control.keyboard.variant, () => this.sendKeyboardMap());
    store.subscribe((s) => s.connection.status, (s) => this.events.emit("connection.status", s));
  }
  /////////////////////////////
  // state
  /////////////////////////////
  // the current state; a new object after every change, never mutated
  get state() {
    return this.store.getState();
  }
  // an immutable update of one slice: patch('control', { locked: true })
  patch(key, part) {
    this.store.setState((s) => ({ ...s, [key]: { ...s[key], ...part } }));
  }
  get connected() {
    return this.state.connection.status === "connected";
  }
  get controlling() {
    return selectControlling(this.state);
  }
  get session() {
    return selectSession(this.state);
  }
  get isAdmin() {
    return selectIsAdmin(this.state);
  }
  get privateModeEnabled() {
    return this.state.settings.private_mode && !this.isAdmin;
  }
  get implicitControl() {
    return this.state.settings.implicit_hosting && !!this.session?.profile.can_host;
  }
  get isTouchDevice() {
    if (this.opts.inputMode)
      return this.opts.inputMode === "touch";
    return ("ontouchstart" in window || navigator.maxTouchPoints > 0) && !matchMedia("(pointer:fine)").matches && !matchMedia("(hover:hover)").matches;
  }
  /////////////////////////////
  // auth
  /////////////////////////////
  // point at a server (defaults to this page); `?token=` in the url logs in with that token
  setUrl(url = location.href) {
    const u = new URL(url);
    const token = u.searchParams.get("token") || void 0;
    const http = (u.origin + u.pathname).replace(/^ws/, "http").replace(/\/(api\/ws\/?)?$/, "");
    this.close();
    this.api.url = http;
    this.store.setState({ connection: { ...this.state.connection, url: http, token }, authenticated: false });
    if (this.opts.autoconnect) {
      this.authenticate().then(() => this.connect()).catch(() => {
      });
    }
  }
  async authenticate(token = this.state.connection.token) {
    if (!token && this.opts.autologin)
      token = localStorage.getItem("neko_session") ?? void 0;
    if (token)
      this.setToken(token);
    await this.api.req("GET", "/whoami");
    this.store.setState({ authenticated: true });
  }
  async login(username, password) {
    const res = await this.api.req("POST", "/login", { username, password });
    this.close();
    if (res.token)
      this.setToken(res.token);
    this.store.setState({ authenticated: true });
  }
  async logout() {
    this.close();
    try {
      await this.api.req("POST", "/logout");
    } finally {
      this.setToken("");
      this.store.setState({ authenticated: false });
    }
  }
  setToken(token) {
    this.api.token = token;
    this.patch("connection", { token: token || void 0 });
    if (!this.opts.autologin)
      return;
    try {
      token ? localStorage.setItem("neko_session", token) : localStorage.removeItem("neko_session");
    } catch {
    }
  }
  /////////////////////////////
  // connection: websocket + transport
  /////////////////////////////
  connect() {
    if (!this.state.authenticated)
      throw new Error("client not authenticated");
    if (this.wanted)
      return;
    this.wanted = true;
    this.attempts = 0;
    this.patch("connection", { status: "connecting" });
    this.openSocket();
  }
  disconnect() {
    this.close();
  }
  // stop everything, the stream included; emits connection.closed if we were connected or connecting
  close(error2) {
    const was = this.wanted;
    this.wanted = false;
    this.introduced = false;
    this.resume = false;
    clearTimeout(this.reconnectTimer);
    clearInterval(this.staleTimer);
    this.reconnectGen++;
    const ws = this.ws;
    this.ws = null;
    ws?.close(1e3);
    this.transport.close();
    this.clear();
    if (was)
      this.events.emit("connection.closed", error2);
  }
  openSocket() {
    const { url, token } = this.state.connection;
    const ws = this.ws = new WebSocket(url.replace(/^http/, "ws") + "/api/ws" + (token ? "?token=" + encodeURIComponent(token) : ""));
    this.lastMessage = Date.now();
    ws.onmessage = (e) => {
      this.lastMessage = Date.now();
      const { event, payload } = JSON.parse(e.data);
      this.onMessage(event, payload);
    };
    ws.onclose = () => this.ws === ws && this.onSocketLost();
    clearInterval(this.staleTimer);
    this.staleTimer = window.setInterval(() => {
      if (this.ws === ws && Date.now() - this.lastMessage > STALE_MS) {
        this.ws = null;
        ws.close();
        this.onSocketLost();
      }
    }, 5e3);
  }
  // the transport is left alone: its stream may well outlive the socket (the last picture stays
  // while the socket reconnects, and a peer that fails meanwhile re-requests itself through
  // session.send, which drops until the socket is back); the reconnect's system/init then
  // starts it over with transport.connect()
  onSocketLost() {
    this.ws = null;
    this.introduced = false;
    this.resume ||= this.state.video.playing;
    if (!this.wanted)
      return;
    this.patch("connection", { status: "connecting" });
    if (++this.attempts > RECONNECT_MAX2)
      return this.close(new Error("connection lost"));
    const gen = this.reconnectGen;
    this.api.req("GET", "/whoami", void 0, { signal: AbortSignal.timeout(5e3) }).then(() => true, (err) => !(err instanceof ApiError && err.status === 401)).then((retry) => {
      if (!this.wanted || gen !== this.reconnectGen)
        return;
      if (!retry)
        return this.close(new Error("session expired"));
      this.reconnectTimer = window.setTimeout(() => this.openSocket(), RECONNECT_BACKOFF_MS2 * Math.min(this.attempts, 4));
    });
  }
  send(event, payload) {
    if (this.ws?.readyState === WebSocket.OPEN)
      this.ws.send(JSON.stringify({ event, payload }));
  }
  // the stream: connected only once the transport is; its status counts while the socket is up
  onTransportState(s) {
    const playable = s.video.playable && !this.state.video.playable;
    this.patch("video", s.video);
    if (playable) {
      if (this.opts.autoplay || this.resume)
        this.transport.setPlaying(true).catch(() => {
        });
      this.resume = false;
    }
    if (!this.wanted || !this.introduced)
      return;
    if (s.status === "connected")
      this.attempts = 0;
    this.patch("connection", {
      status: s.status === "connected" || s.status === "unavailable" ? "connected" : "connecting"
    });
  }
  // what the transport gets of this session, once the server has introduced it (system/init)
  sessionInfo(init) {
    return {
      url: this.state.connection.url,
      token: this.state.connection.token,
      init,
      send: (event, payload) => this.send(event, payload),
      on: (event, cb) => this.messages.on(event, cb)
    };
  }
  /////////////////////////////
  // websocket events -> state
  /////////////////////////////
  // every message goes to the transport's subscribers first (signal/*); the rest is the room.
  // What one message changes is one setState, so subscribers never see it half-applied.
  onMessage(event, p) {
    this.messages.emit(event, p);
    const { state, store, events } = this;
    switch (event) {
      case "system/init":
        store.setState({
          session_id: p.session_id,
          control: {
            ...state.control,
            touch: !!p.touch_events,
            host_id: p.control_host?.has_host ? p.control_host.host_id : null
          },
          screen: { ...state.screen, size: p.screen_size },
          sessions: p.sessions ?? {},
          settings: p.settings
        });
        this.introduced = true;
        this.transport.connect(this.sessionInfo(p)).catch((err) => this.close(err));
        break;
      case "system/admin":
        this.patch("screen", {
          configurations: [...p.screen_sizes_list ?? []].sort((a, b) => b.width - a.width || b.height - a.height || b.rate - a.rate)
        });
        events.emit("room.broadcast.status", p.broadcast_status?.is_active, p.broadcast_status?.url);
        break;
      case "system/settings": {
        const { id, ...settings } = p;
        store.setState({ settings });
        events.emit("room.settings.updated", settings, id);
        break;
      }
      case "system/disconnect":
        this.close(new Error(p.message));
        break;
      case "system/heartbeat":
        this.send("client/heartbeat");
        break;
      case "session/created":
        store.setState({ sessions: { ...state.sessions, [p.id]: p } });
        events.emit("session.created", p.id);
        break;
      case "session/deleted": {
        const sessions = { ...state.sessions };
        delete sessions[p.id];
        store.setState({ sessions });
        events.emit("session.deleted", p.id);
        break;
      }
      case "session/profile": {
        const { id, ...profile } = p;
        if (!state.sessions[id])
          return;
        store.setState({ sessions: { ...state.sessions, [id]: { ...state.sessions[id], profile } } });
        events.emit("session.updated", id);
        break;
      }
      case "session/state": {
        const { id, ...st } = p;
        if (!state.sessions[id])
          return;
        store.setState({ sessions: { ...state.sessions, [id]: { ...state.sessions[id], state: st } } });
        events.emit("session.updated", id);
        break;
      }
      case "session/cursors":
        break;
      // inactive cursors: not shown by this client
      case "control/host":
        this.patch("control", { host_id: p.has_host ? p.host_id : null });
        events.emit("room.control.host", p.has_host, p.host_id, p.id);
        break;
      case "control/request":
        events.emit("room.control.request", p.id);
        break;
      case "screen/updated": {
        const { id, ...size } = p;
        this.patch("screen", { size });
        events.emit("room.screen.updated", size.width, size.height, size.rate, id);
        break;
      }
      case "clipboard/updated":
        this.patch("control", { clipboard: { text: p.text } });
        this.clipboardAt = Date.now();
        navigator.clipboard?.writeText(p.text).catch(() => {
        });
        events.emit("room.clipboard.updated", p.text);
        break;
      case "broadcast/status":
        events.emit("room.broadcast.status", p.is_active, p.url);
        break;
      case "send/unicast":
        events.emit("receive.unicast", p.sender, p.subject, p.body);
        break;
      case "send/broadcast":
        events.emit("receive.broadcast", p.sender, p.subject, p.body);
        break;
      default:
        events.emit("message", event, p);
    }
  }
  clear() {
    const { state } = this;
    this.store.setState({
      connection: { ...state.connection, status: "disconnected" },
      control: { ...state.control, host_id: null, clipboard: null },
      screen: { ...state.screen, configurations: [] },
      session_id: null,
      sessions: {},
      settings: defaultSettings()
    });
  }
  /////////////////////////////
  // control
  /////////////////////////////
  request() {
    this.send("control/request");
  }
  release() {
    this.send("control/release");
  }
  // local lock: keep control but stop sending input
  lock() {
    this.patch("control", { locked: true });
  }
  unlock() {
    this.patch("control", { locked: false });
  }
  // types text remotely via the server's clipboard
  paste(text) {
    this.send("control/paste", { text });
  }
  // Before a paste keystroke, with the text the browser's own paste event handed over: the side
  // that copied most recently wins. The remote reports every change of its clipboard; the local
  // clipboard can only be looked at when the browser hands it over, so if the remote changed since
  // the local text was last seen, the remote is taken as fresher and nothing is sent (on https the
  // remote text is also written to the local clipboard, so the two then match). Otherwise the
  // local text becomes the remote clipboard, and this resolves once the server has confirmed it.
  async preparePaste(text) {
    const stale = this.localSeen.at < this.clipboardAt;
    const fresh = text !== "" && text !== this.state.control.clipboard?.text && !stale;
    this.localSeen = { text, at: Date.now() };
    if (!fresh)
      return;
    this.send("clipboard/set", { text });
    await new Promise((done) => {
      const off = this.events.on("room.clipboard.updated", () => (off(), done()));
      setTimeout(() => (off(), done()), 500);
    });
  }
  setScrollInverse(value = true) {
    this.patch("control", { scroll: { ...this.state.control.scroll, inverse: value } });
  }
  setScrollSensitivity(value) {
    this.patch("control", { scroll: { ...this.state.control.scroll, sensitivity: value } });
  }
  setKeyboard(layout, variant = "") {
    this.patch("control", { keyboard: { layout, variant } });
  }
  sendKeyboardMap() {
    if (this.controlling && this.state.control.keyboard.layout)
      this.send("keyboard/map", this.state.control.keyboard);
  }
  setScreenSize(width, height, rate) {
    this.send("screen/set", { width, height, rate });
  }
  sendBroadcast(subject, body) {
    this.send("send/broadcast", { subject, body });
  }
  sendUnicast(receiver, subject, body) {
    this.send("send/unicast", { receiver, subject, body });
  }
  async uploadDrop({ x, y, files }) {
    const form = new FormData();
    form.append("x", String(x));
    form.append("y", String(y));
    for (const f of files)
      form.append("files", f);
    try {
      await this.api.upload("/room/upload/drop", form, (p) => this.events.emit("upload.drop.progress", p));
      this.events.emit("upload.drop.finished");
    } catch (err) {
      this.events.emit("upload.drop.finished", err);
    }
  }
  /////////////////////////////
  // media (through the transport)
  /////////////////////////////
  // when the browser refuses sound, playback starts muted (video.mutedByAutoplay) and unmutes on
  // the first click
  play() {
    return this.transport.setPlaying(true);
  }
  pause() {
    this.transport.setPlaying(false);
  }
  mute() {
    this.transport.setMuted(true);
  }
  unmute() {
    this.transport.setMuted(false);
  }
  setVolume(value) {
    this.transport.setVolume(Math.max(0, Math.min(1, value)));
  }
  // share local media (microphone), on transports that can; returns what stops sharing it
  shareMedia(stream) {
    if (!this.transport.shareMedia)
      throw new Error("transport cannot send media");
    return this.transport.shareMedia(stream);
  }
};

// node_modules/@m1k1o/neko/dist/keyboard/guacamole.js
var Guacamole = Guacamole || {};
Guacamole.Keyboard = function Keyboard(element) {
  var guac_keyboard = this;
  var guacKeyboardID = Guacamole.Keyboard._nextID++;
  var EVENT_MARKER = "_GUAC_KEYBOARD_HANDLED_BY_" + guacKeyboardID;
  this.onkeydown = null;
  this.onkeyup = null;
  var quirks = {
    /**
     * Whether keyup events are universally unreliable.
     *
     * @type {!boolean}
     */
    keyupUnreliable: false,
    /**
     * Whether the Alt key is actually a modifier for typable keys and is
     * thus never used for keyboard shortcuts.
     *
     * @type {!boolean}
     */
    altIsTypableOnly: false,
    /**
     * Whether we can rely on receiving a keyup or keydown event for the
     * Caps Lock key.
     *
     * @type {!boolean}
     */
    capsLockKeyEventUnreliable: false
  };
  if (navigator && navigator.platform) {
    if (navigator.platform.match(/ipad|iphone|ipod/i))
      quirks.keyupUnreliable = true;
    else if (navigator.platform.match(/^mac/i)) {
      quirks.altIsTypableOnly = true;
      quirks.capsLockKeyEventUnreliable = true;
    }
  }
  var KeyEvent = function KeyEvent2(orig) {
    var key_event = this;
    this.keyCode = orig ? orig.which || orig.keyCode : 0;
    this.keyIdentifier = orig && orig.keyIdentifier;
    this.key = orig && orig.key;
    this.location = orig ? getEventLocation(orig) : 0;
    this.modifiers = orig ? Guacamole.Keyboard.ModifierState.fromKeyboardEvent(orig) : new Guacamole.Keyboard.ModifierState();
    this.timestamp = (/* @__PURE__ */ new Date()).getTime();
    this.defaultPrevented = false;
    this.keysym = null;
    this.reliable = false;
    this.getAge = function() {
      return (/* @__PURE__ */ new Date()).getTime() - key_event.timestamp;
    };
  };
  var KeydownEvent = function KeydownEvent2(orig) {
    KeyEvent.call(this, orig);
    this.keysym = keysym_from_key_identifier(this.key, this.location) || keysym_from_keycode(this.keyCode, this.location);
    this.keyupReliable = !quirks.keyupUnreliable;
    if (this.keysym && !isPrintable(this.keysym))
      this.reliable = true;
    if (!this.keysym && key_identifier_sane(this.keyCode, this.keyIdentifier))
      this.keysym = keysym_from_key_identifier(this.keyIdentifier, this.location, this.modifiers.shift);
    if (this.modifiers.meta && this.keysym !== 65511 && this.keysym !== 65512)
      this.keyupReliable = false;
    else if (this.keysym === 65509 && quirks.capsLockKeyEventUnreliable)
      this.keyupReliable = false;
    var prevent_alt = !this.modifiers.ctrl && !quirks.altIsTypableOnly;
    if (quirks.altIsTypableOnly && (this.keysym === 65513 || this.keysym === 65514))
      this.keysym = 65027;
    var prevent_ctrl = !this.modifiers.alt;
    if (prevent_ctrl && this.modifiers.ctrl || prevent_alt && this.modifiers.alt || this.modifiers.meta || this.modifiers.hyper)
      this.reliable = true;
    recentKeysym[this.keyCode] = this.keysym;
  };
  KeydownEvent.prototype = new KeyEvent();
  var KeypressEvent = function KeypressEvent2(orig) {
    KeyEvent.call(this, orig);
    this.keysym = keysym_from_charcode(this.keyCode);
    this.reliable = true;
  };
  KeypressEvent.prototype = new KeyEvent();
  var KeyupEvent = function KeyupEvent2(orig) {
    KeyEvent.call(this, orig);
    if (this.keyCode == 20 && quirks.capsLockKeyEventUnreliable) {
      eventLog.push(new KeydownEvent(this));
      return;
    }
    this.keysym = keysym_from_keycode(this.keyCode, this.location) || keysym_from_key_identifier(this.key, this.location);
    if (!guac_keyboard.pressed[this.keysym])
      this.keysym = recentKeysym[this.keyCode] || this.keysym;
    this.reliable = true;
  };
  KeyupEvent.prototype = new KeyEvent();
  var eventLog = [];
  var keycodeKeysyms = {
    8: [65288],
    // backspace
    9: [65289],
    // tab
    12: [65291, 65291, 65291, 65461],
    // clear       / KP 5
    13: [65293],
    // enter
    16: [65505, 65505, 65506],
    // shift
    17: [65507, 65507, 65508],
    // ctrl
    18: [65513, 65513, 65514],
    // alt
    19: [65299],
    // pause/break
    20: [65509],
    // caps lock
    27: [65307],
    // escape
    32: [32],
    // space
    33: [65365, 65365, 65365, 65465],
    // page up     / KP 9
    34: [65366, 65366, 65366, 65459],
    // page down   / KP 3
    35: [65367, 65367, 65367, 65457],
    // end         / KP 1
    36: [65360, 65360, 65360, 65463],
    // home        / KP 7
    37: [65361, 65361, 65361, 65460],
    // left arrow  / KP 4
    38: [65362, 65362, 65362, 65464],
    // up arrow    / KP 8
    39: [65363, 65363, 65363, 65462],
    // right arrow / KP 6
    40: [65364, 65364, 65364, 65458],
    // down arrow  / KP 2
    45: [65379, 65379, 65379, 65456],
    // insert      / KP 0
    46: [65535, 65535, 65535, 65454],
    // delete      / KP decimal
    91: [65511],
    // left windows/command key (meta_l)
    92: [65512],
    // right window/command key (meta_r)
    93: [65383],
    // menu key
    96: [65456],
    // KP 0
    97: [65457],
    // KP 1
    98: [65458],
    // KP 2
    99: [65459],
    // KP 3
    100: [65460],
    // KP 4
    101: [65461],
    // KP 5
    102: [65462],
    // KP 6
    103: [65463],
    // KP 7
    104: [65464],
    // KP 8
    105: [65465],
    // KP 9
    106: [65450],
    // KP multiply
    107: [65451],
    // KP add
    109: [65453],
    // KP subtract
    110: [65454],
    // KP decimal
    111: [65455],
    // KP divide
    112: [65470],
    // f1
    113: [65471],
    // f2
    114: [65472],
    // f3
    115: [65473],
    // f4
    116: [65474],
    // f5
    117: [65475],
    // f6
    118: [65476],
    // f7
    119: [65477],
    // f8
    120: [65478],
    // f9
    121: [65479],
    // f10
    122: [65480],
    // f11
    123: [65481],
    // f12
    144: [65407],
    // num lock
    145: [65300],
    // scroll lock
    225: [65027]
    // altgraph (iso_level3_shift)
  };
  var keyidentifier_keysym = {
    "Again": [65382],
    "AllCandidates": [65341],
    "Alphanumeric": [65328],
    "Alt": [65513, 65513, 65514],
    "Attn": [64782],
    "AltGraph": [65027],
    "ArrowDown": [65364],
    "ArrowLeft": [65361],
    "ArrowRight": [65363],
    "ArrowUp": [65362],
    "Backspace": [65288],
    "CapsLock": [65509],
    "Cancel": [65385],
    "Clear": [65291],
    "Convert": [65315],
    "Copy": [64789],
    "Crsel": [64796],
    "CrSel": [64796],
    "CodeInput": [65335],
    "Compose": [65312],
    "Control": [65507, 65507, 65508],
    "ContextMenu": [65383],
    "Delete": [65535],
    "Down": [65364],
    "End": [65367],
    "Enter": [65293],
    "EraseEof": [64774],
    "Escape": [65307],
    "Execute": [65378],
    "Exsel": [64797],
    "ExSel": [64797],
    "F1": [65470],
    "F2": [65471],
    "F3": [65472],
    "F4": [65473],
    "F5": [65474],
    "F6": [65475],
    "F7": [65476],
    "F8": [65477],
    "F9": [65478],
    "F10": [65479],
    "F11": [65480],
    "F12": [65481],
    "F13": [65482],
    "F14": [65483],
    "F15": [65484],
    "F16": [65485],
    "F17": [65486],
    "F18": [65487],
    "F19": [65488],
    "F20": [65489],
    "F21": [65490],
    "F22": [65491],
    "F23": [65492],
    "F24": [65493],
    "Find": [65384],
    "GroupFirst": [65036],
    "GroupLast": [65038],
    "GroupNext": [65032],
    "GroupPrevious": [65034],
    "FullWidth": null,
    "HalfWidth": null,
    "HangulMode": [65329],
    "Hankaku": [65321],
    "HanjaMode": [65332],
    "Help": [65386],
    "Hiragana": [65317],
    "HiraganaKatakana": [65319],
    "Home": [65360],
    "Hyper": [65517, 65517, 65518],
    "Insert": [65379],
    "JapaneseHiragana": [65317],
    "JapaneseKatakana": [65318],
    "JapaneseRomaji": [65316],
    "JunjaMode": [65336],
    "KanaMode": [65325],
    "KanjiMode": [65313],
    "Katakana": [65318],
    "Left": [65361],
    "Meta": [65511, 65511, 65512],
    "ModeChange": [65406],
    "NonConvert": [65314],
    "NumLock": [65407],
    "PageDown": [65366],
    "PageUp": [65365],
    "Pause": [65299],
    "Play": [64790],
    "PreviousCandidate": [65342],
    "PrintScreen": [65377],
    "Redo": [65382],
    "Right": [65363],
    "Romaji": [65316],
    "RomanCharacters": null,
    "Scroll": [65300],
    "Select": [65376],
    "Separator": [65452],
    "Shift": [65505, 65505, 65506],
    "SingleCandidate": [65340],
    "Super": [65515, 65515, 65516],
    "Tab": [65289],
    "UIKeyInputDownArrow": [65364],
    "UIKeyInputEscape": [65307],
    "UIKeyInputLeftArrow": [65361],
    "UIKeyInputRightArrow": [65363],
    "UIKeyInputUpArrow": [65362],
    "Up": [65362],
    "Undo": [65381],
    "Win": [65511, 65511, 65512],
    "Zenkaku": [65320],
    "ZenkakuHankaku": [65322]
  };
  var no_repeat = {
    65027: true,
    // ISO Level 3 Shift (AltGr)
    65505: true,
    // Left shift
    65506: true,
    // Right shift
    65507: true,
    // Left ctrl 
    65508: true,
    // Right ctrl 
    65509: true,
    // Caps Lock
    65511: true,
    // Left meta 
    65512: true,
    // Right meta 
    65513: true,
    // Left alt
    65514: true,
    // Right alt
    65515: true,
    // Left super/hyper
    65516: true
    // Right super/hyper
  };
  this.modifiers = new Guacamole.Keyboard.ModifierState();
  this.pressed = {};
  var implicitlyPressed = {};
  var last_keydown_result = {};
  var recentKeysym = {};
  var key_repeat_timeout = null;
  var key_repeat_interval = null;
  var get_keysym = function get_keysym2(keysyms, location2) {
    if (!keysyms)
      return null;
    return keysyms[location2] || keysyms[0];
  };
  var isPrintable = function isPrintable2(keysym) {
    return keysym >= 0 && keysym <= 255 || (keysym & 4294901760) === 16777216;
  };
  function keysym_from_key_identifier(identifier, location2, shifted) {
    if (!identifier)
      return null;
    var typedCharacter;
    var unicodePrefixLocation = identifier.indexOf("U+");
    if (unicodePrefixLocation >= 0) {
      var hex = identifier.substring(unicodePrefixLocation + 2);
      typedCharacter = String.fromCharCode(parseInt(hex, 16));
    } else if (identifier.length === 1 && location2 !== 3)
      typedCharacter = identifier;
    else
      return get_keysym(keyidentifier_keysym[identifier], location2);
    if (shifted === true)
      typedCharacter = typedCharacter.toUpperCase();
    else if (shifted === false)
      typedCharacter = typedCharacter.toLowerCase();
    var codepoint = typedCharacter.charCodeAt(0);
    return keysym_from_charcode(codepoint);
  }
  function isControlCharacter(codepoint) {
    return codepoint <= 31 || codepoint >= 127 && codepoint <= 159;
  }
  function keysym_from_charcode(codepoint) {
    if (isControlCharacter(codepoint)) return 65280 | codepoint;
    if (codepoint >= 0 && codepoint <= 255)
      return codepoint;
    if (codepoint >= 256 && codepoint <= 1114111)
      return 16777216 | codepoint;
    return null;
  }
  function keysym_from_keycode(keyCode, location2) {
    return get_keysym(keycodeKeysyms[keyCode], location2);
  }
  var key_identifier_sane = function key_identifier_sane2(keyCode, keyIdentifier) {
    if (!keyIdentifier)
      return false;
    var unicodePrefixLocation = keyIdentifier.indexOf("U+");
    if (unicodePrefixLocation === -1)
      return true;
    var codepoint = parseInt(keyIdentifier.substring(unicodePrefixLocation + 2), 16);
    if (keyCode !== codepoint)
      return true;
    if (keyCode >= 65 && keyCode <= 90 || keyCode >= 48 && keyCode <= 57)
      return true;
    return false;
  };
  this.press = function(keysym) {
    if (keysym === null) return;
    if (!guac_keyboard.pressed[keysym]) {
      guac_keyboard.pressed[keysym] = true;
      if (guac_keyboard.onkeydown) {
        var result = guac_keyboard.onkeydown(keysym);
        last_keydown_result[keysym] = result;
        window.clearTimeout(key_repeat_timeout);
        window.clearInterval(key_repeat_interval);
        if (!no_repeat[keysym])
          key_repeat_timeout = window.setTimeout(function() {
            key_repeat_interval = window.setInterval(function() {
              guac_keyboard.onkeyup(keysym);
              guac_keyboard.onkeydown(keysym);
            }, 50);
          }, 500);
        return result;
      }
    }
    return last_keydown_result[keysym] || false;
  };
  this.release = function(keysym) {
    if (guac_keyboard.pressed[keysym]) {
      delete guac_keyboard.pressed[keysym];
      delete implicitlyPressed[keysym];
      window.clearTimeout(key_repeat_timeout);
      window.clearInterval(key_repeat_interval);
      if (keysym !== null && guac_keyboard.onkeyup)
        guac_keyboard.onkeyup(keysym);
    }
  };
  this.type = function type(str) {
    for (var i = 0; i < str.length; i++) {
      var codepoint = str.codePointAt ? str.codePointAt(i) : str.charCodeAt(i);
      var keysym = keysym_from_charcode(codepoint);
      guac_keyboard.press(keysym);
      guac_keyboard.release(keysym);
    }
  };
  this.reset = function() {
    for (var keysym in guac_keyboard.pressed)
      guac_keyboard.release(parseInt(keysym));
    eventLog = [];
  };
  var updateModifierState = function updateModifierState2(modifier, keysyms, keyEvent) {
    var localState = keyEvent.modifiers[modifier];
    var remoteState = guac_keyboard.modifiers[modifier];
    var i;
    if (keysyms.indexOf(keyEvent.keysym) !== -1)
      return;
    if (remoteState && localState === false) {
      for (i = 0; i < keysyms.length; i++) {
        guac_keyboard.release(keysyms[i]);
      }
    } else if (!remoteState && localState) {
      for (i = 0; i < keysyms.length; i++) {
        if (guac_keyboard.pressed[keysyms[i]])
          return;
      }
      var keysym = keysyms[0];
      if (keyEvent.keysym)
        implicitlyPressed[keysym] = true;
      guac_keyboard.press(keysym);
    }
  };
  var syncModifierStates = function syncModifierStates2(keyEvent) {
    updateModifierState("alt", [
      65513,
      // Left alt
      65514,
      // Right alt
      65027
      // AltGr
    ], keyEvent);
    updateModifierState("shift", [
      65505,
      // Left shift
      65506
      // Right shift
    ], keyEvent);
    updateModifierState("ctrl", [
      65507,
      // Left ctrl
      65508
      // Right ctrl
    ], keyEvent);
    updateModifierState("meta", [
      65511,
      // Left meta
      65512
      // Right meta
    ], keyEvent);
    updateModifierState("hyper", [
      65515,
      // Left super/hyper
      65516
      // Right super/hyper
    ], keyEvent);
    guac_keyboard.modifiers = keyEvent.modifiers;
  };
  var isStateImplicit = function isStateImplicit2() {
    for (var keysym in guac_keyboard.pressed) {
      if (!implicitlyPressed[keysym])
        return false;
    }
    return true;
  };
  function interpret_events() {
    var handled_event = interpret_event();
    if (!handled_event)
      return false;
    var last_event;
    do {
      last_event = handled_event;
      handled_event = interpret_event();
    } while (handled_event !== null);
    if (isStateImplicit())
      guac_keyboard.reset();
    return last_event.defaultPrevented;
  }
  var release_simulated_altgr = function release_simulated_altgr2(keysym) {
    if (!guac_keyboard.modifiers.ctrl || !guac_keyboard.modifiers.alt)
      return;
    if (keysym >= 65 && keysym <= 90)
      return;
    if (keysym >= 97 && keysym <= 122)
      return;
    if (keysym <= 255 || (keysym & 4278190080) === 16777216) {
      guac_keyboard.release(65507);
      guac_keyboard.release(65508);
      guac_keyboard.release(65513);
      guac_keyboard.release(65514);
    }
  };
  var interpret_event = function interpret_event2() {
    var first = eventLog[0];
    if (!first)
      return null;
    if (first instanceof KeydownEvent) {
      var keysym = null;
      var accepted_events = [];
      if (first.keysym === 65511 || first.keysym === 65512) {
        if (eventLog.length === 1)
          return null;
        if (eventLog[1].keysym !== first.keysym) {
          if (!eventLog[1].modifiers.meta)
            return eventLog.shift();
        } else if (eventLog[1] instanceof KeydownEvent)
          return eventLog.shift();
      }
      if (first.reliable) {
        keysym = first.keysym;
        accepted_events = eventLog.splice(0, 1);
      } else if (eventLog[1] instanceof KeypressEvent) {
        keysym = eventLog[1].keysym;
        accepted_events = eventLog.splice(0, 2);
      } else if (eventLog[1]) {
        keysym = first.keysym;
        accepted_events = eventLog.splice(0, 1);
      }
      if (accepted_events.length > 0) {
        syncModifierStates(first);
        if (keysym) {
          release_simulated_altgr(keysym);
          var defaultPrevented = !guac_keyboard.press(keysym);
          recentKeysym[first.keyCode] = keysym;
          if (!first.keyupReliable)
            guac_keyboard.release(keysym);
          for (var i = 0; i < accepted_events.length; i++)
            accepted_events[i].defaultPrevented = defaultPrevented;
        }
        return first;
      }
    } else if (first instanceof KeyupEvent && !quirks.keyupUnreliable) {
      var keysym = first.keysym;
      if (keysym) {
        guac_keyboard.release(keysym);
        delete recentKeysym[first.keyCode];
        first.defaultPrevented = true;
      } else {
        guac_keyboard.reset();
        return first;
      }
      syncModifierStates(first);
      return eventLog.shift();
    } else
      return eventLog.shift();
    return null;
  };
  var getEventLocation = function getEventLocation2(e) {
    if ("location" in e)
      return e.location;
    if ("keyLocation" in e)
      return e.keyLocation;
    return 0;
  };
  var markEvent = function markEvent2(e) {
    if (e[EVENT_MARKER])
      return false;
    e[EVENT_MARKER] = true;
    return true;
  };
  this.listenTo = function listenTo(element2) {
    element2.addEventListener("keydown", function(e) {
      if (!guac_keyboard.onkeydown) return;
      if (!markEvent(e)) return;
      var keydownEvent = new KeydownEvent(e);
      if (e.isComposing || keydownEvent.keyCode === 229)
        return;
      eventLog.push(keydownEvent);
      if (interpret_events())
        e.preventDefault();
    }, true);
    element2.addEventListener("keypress", function(e) {
      if (!guac_keyboard.onkeydown && !guac_keyboard.onkeyup) return;
      if (!markEvent(e)) return;
      eventLog.push(new KeypressEvent(e));
      if (interpret_events())
        e.preventDefault();
    }, true);
    element2.addEventListener("keyup", function(e) {
      if (!guac_keyboard.onkeyup) return;
      if (!markEvent(e)) return;
      e.preventDefault();
      eventLog.push(new KeyupEvent(e));
      interpret_events();
    }, true);
    var handleInput = function handleInput2(e) {
      if (!guac_keyboard.onkeydown && !guac_keyboard.onkeyup) return;
      if (!markEvent(e)) return;
      if (e.data && !e.isComposing)
        guac_keyboard.type(e.data);
    };
    var handleCompositionStart = function handleCompositionStart2(e) {
      element2.removeEventListener("input", handleInput, false);
    };
    var handleCompositionEnd = function handleCompositionEnd2(e) {
      if (!guac_keyboard.onkeydown && !guac_keyboard.onkeyup) return;
      if (!markEvent(e)) return;
      if (e.data)
        guac_keyboard.type(e.data);
    };
    element2.addEventListener("input", handleInput, false);
    element2.addEventListener("compositionend", handleCompositionEnd, false);
    element2.addEventListener("compositionstart", handleCompositionStart, false);
  };
  if (element)
    guac_keyboard.listenTo(element);
};
Guacamole.Keyboard._nextID = 0;
Guacamole.Keyboard.ModifierState = function() {
  this.shift = false;
  this.ctrl = false;
  this.alt = false;
  this.meta = false;
  this.hyper = false;
};
Guacamole.Keyboard.ModifierState.fromKeyboardEvent = function(e) {
  var state = new Guacamole.Keyboard.ModifierState();
  state.shift = e.shiftKey;
  state.ctrl = e.ctrlKey;
  state.alt = e.altKey;
  state.meta = e.metaKey;
  if (e.getModifierState) {
    state.hyper = e.getModifierState("OS") || e.getModifierState("Super") || e.getModifierState("Hyper") || e.getModifierState("Win");
  }
  return state;
};
var guacamole_default = Guacamole.Keyboard;

// node_modules/@m1k1o/neko/dist/overlay.js
var WHEEL_STEP = 53;
var WHEEL_LINE_HEIGHT = 19;
var MOVE_THROTTLE_MS = 1e3 / 60;
var LAYER = "position:absolute;inset:0;width:100%;height:100%;";
var XK = {
  v: 118,
  Control_L: 65507,
  Control_R: 65508,
  Meta_L: 65511,
  Alt_L: 65513,
  Alt_R: 65514,
  Super_L: 65515,
  Super_R: 65516,
  Mode_switch: 65406,
  ISO_Level3_Shift: 65027
};
var isMac = /Mac|iPhone|iPod|iPad/i.test(navigator.userAgent);
var MAC_REMAP = {
  [XK.Meta_L]: XK.Control_L,
  [XK.Super_L]: XK.Alt_L,
  [XK.Super_R]: XK.Super_L,
  [XK.Alt_L]: XK.Mode_switch,
  [XK.Alt_R]: XK.ISO_Level3_Shift
};
var remap = (key) => isMac && MAC_REMAP[key] || key;
var isCtrl = (key) => key === XK.Control_L || key === XK.Control_R;
var TOUCH_PHASE = {
  touchstart: "begin",
  touchmove: "update",
  touchend: "end",
  touchcancel: "end"
};
var Overlay = class {
  wrap = document.createElement("div");
  canvas = document.createElement("canvas");
  ctx = this.canvas.getContext("2d");
  input = document.createElement("textarea");
  keyboard = new guacamole_default();
  cleanup = [];
  box = null;
  size = { width: 0, height: 0 };
  // of the box, in CSS px
  cursor = null;
  cursorImg = new Image();
  cursorPos = null;
  drawQueued = false;
  wheel = { x: 0, y: 0, t: 0 };
  lastMove = 0;
  moveTimer = 0;
  mouseDown = false;
  pendingPress = null;
  // implicit hosting: replayed once we are host
  ctrlDown = 0;
  noKeyUp = /* @__PURE__ */ new Set();
  client;
  constructor(client2) {
    this.client = client2;
    this.canvas.style.cssText = LAYER;
    this.input.style.cssText = LAYER + "font-size:16px;resize:none;caret-color:transparent;outline:0;border:0;color:transparent;background:transparent;";
    this.input.spellcheck = false;
    this.input.setAttribute("autocapitalize", "off");
    this.input.setAttribute("autocomplete", "off");
    this.input.setAttribute("data-gramm", "false");
    this.wrap.append(this.canvas, this.input);
    this.keyboard.onkeydown = (key) => this.onKeyDown(remap(key));
    this.keyboard.onkeyup = (key) => this.onKeyUp(remap(key));
    this.keyboard.listenTo(this.input);
    this.cursorImg.onload = this.draw;
  }
  // `box` is a positioned element that already holds the transport's element (so the stream is
  // below): the overlay fills it, and keeps it letterboxed inside its parent to the remote screen's
  // aspect ratio so that its canvas maps onto the picture (a stream alone needs no letterboxing, the
  // <video> keeps the aspect ratio by itself). Returns what detaches it again; a second attach
  // replaces the first.
  attach(box) {
    this.detach();
    this.box = box;
    box.append(this.wrap);
    const { client: client2, input } = this;
    const on = (el, ev, fn, opts) => {
      el.addEventListener(ev, fn, opts);
      this.cleanup.push(() => el.removeEventListener(ev, fn, opts));
    };
    const stop = (fn) => (e) => {
      e.preventDefault();
      e.stopPropagation();
      fn(e);
    };
    on(input, "click", stop((e) => client2.events.emit("overlay.click", e)));
    on(input, "contextmenu", stop(() => {
    }));
    on(input, "wheel", stop((e) => this.onWheel(e)), { passive: false });
    on(input, "mousemove", stop((e) => this.onMove(e)));
    on(input, "mousedown", stop((e) => this.onButton(e, true)));
    on(window, "mouseup", (e) => this.onButton(e, false), { capture: true });
    on(input, "mouseenter", (e) => this.onEnter(e));
    on(input, "mouseleave", () => {
      this.hovering = false;
      this.keyboard.reset();
    });
    for (const ev of Object.keys(TOUCH_PHASE))
      on(input, ev, (e) => this.onTouch(e), { passive: false });
    on(input, "dragover", stop((e) => this.onMove(e)));
    on(input, "drop", stop((e) => this.onDrop(e)));
    on(input, "input", (e) => !e.isComposing && (this.input.value = ""));
    on(input, "compositionend", () => this.input.value = "");
    on(input, "paste", stop((e) => this.onPaste(e.clipboardData?.getData("text/plain") ?? "")));
    on(input, "blur", () => client2.store.setState({ mobile_keyboard_open: false }));
    const observer = new ResizeObserver(() => this.onResize());
    if (box.parentElement)
      observer.observe(box.parentElement);
    this.cleanup.push(() => observer.disconnect(), client2.store.subscribe((s) => s.screen.size, () => this.onResize()), client2.store.subscribe(() => this.visible, () => this.sync()), client2.store.subscribe(() => this.interactive, () => this.sync()), client2.store.subscribe(selectControlling, (c) => this.onControl(c)), client2.store.subscribe((s) => s.control.host_id, this.draw), client2.store.subscribe(() => this.active, () => this.focusIfActive()), client2.transport.on("cursor.position", (p) => p ? this.onCursorPosition(p) : this.clearCursor()), client2.transport.on("cursor.image", (img) => this.onCursorImage(img)));
    this.sync();
    this.onResize();
    return () => this.detach();
  }
  detach() {
    if (!this.box)
      return;
    clearTimeout(this.moveTimer);
    this.cleanup.splice(0).forEach((fn) => fn());
    this.keyboard.reset();
    this.wrap.remove();
    this.box = null;
  }
  // shown unless the room is private (to non-admins) or there is no connection; takes the mouse
  // only while this member may host and has not locked themselves out
  get visible() {
    return !this.client.privateModeEnabled && this.client.state.connection.status !== "disconnected";
  }
  get interactive() {
    return !this.client.state.control.locked && !!this.client.session?.profile.can_host;
  }
  sync() {
    this.wrap.style.display = this.visible ? "" : "none";
    this.wrap.style.pointerEvents = this.interactive ? "auto" : "none";
  }
  // letterbox the box to the remote screen's aspect ratio inside its parent
  onResize() {
    const area = this.box?.parentElement;
    if (!this.box || !area)
      return;
    const { width, height } = this.client.state.screen.size;
    const { offsetWidth: W, offsetHeight: H } = area;
    const w = Math.min(W, H * width / height);
    const h = Math.min(H, W * height / width);
    Object.assign(this.box.style, {
      width: `${w}px`,
      height: `${h}px`,
      marginTop: `${(H - h) / 2}px`,
      marginLeft: `${(W - w) / 2}px`
    });
    this.size = { width: w, height: h };
    this.resize();
  }
  // input only flows while we are host and have not locked ourselves out
  get active() {
    return this.client.controlling && !this.client.state.control.locked;
  }
  pos(e) {
    const r = this.canvas.getBoundingClientRect();
    const { width, height } = this.client.state.screen.size;
    const clamp = (v, max) => Math.max(0, Math.min(Math.round(v), max));
    return {
      x: clamp(width / r.width * (e.clientX - r.left), width),
      y: clamp(height / r.height * (e.clientY - r.top), height)
    };
  }
  move(p) {
    this.client.input.move(p.x, p.y);
  }
  button(code, down) {
    this.client.input.button(code, down);
  }
  /////////////////////////////
  // mouse
  /////////////////////////////
  // throttled to MOVE_THROTTLE_MS, but the last position always goes out (trailing send),
  // otherwise the remote pointer stops short of where the local one came to rest
  onMove(e) {
    if (!this.active)
      return;
    const pos2 = this.pos(e);
    clearTimeout(this.moveTimer);
    const wait = MOVE_THROTTLE_MS - (performance.now() - this.lastMove);
    if (wait <= 0)
      return this.sendMove(pos2);
    this.moveTimer = window.setTimeout(() => this.sendMove(pos2), wait);
  }
  sendMove(pos2) {
    this.lastMove = performance.now();
    if (this.active)
      this.move(pos2);
  }
  onButton(e, down) {
    if (down)
      this.mouseDown = true;
    else if (!this.mouseDown)
      return;
    else
      this.mouseDown = false;
    if (!this.client.controlling) {
      if (down && this.client.implicitControl) {
        this.pendingPress = { e, at: performance.now() };
        this.client.request();
      }
      return;
    }
    if (this.client.state.control.locked)
      return;
    this.move(this.pos(e));
    this.button(e.button + 1, down);
  }
  onWheel(e) {
    if (!this.active)
      return;
    const first = e.timeStamp - this.wheel.t > 250;
    if (first)
      this.wheel = { x: 0, y: 0, t: e.timeStamp };
    const k = e.deltaMode !== 0 ? WHEEL_LINE_HEIGHT : 1;
    this.wheel.x += e.deltaX * k;
    this.wheel.y += e.deltaY * k;
    const { sensitivity, inverse } = this.client.state.control.scroll;
    const step = sensitivity < 0 ? WHEEL_STEP * (1 - sensitivity) : WHEEL_STEP;
    const amount = (sensitivity > 0 ? sensitivity + 1 : 1) * (inverse ? -1 : 1);
    const axis = (d) => {
      const v = this.wheel[d];
      if (!first && Math.abs(v) < step)
        return 0;
      if (!first)
        this.wheel[d] = 0;
      return Math.sign(v) * amount;
    };
    const dx = axis("x");
    const dy = axis("y");
    if (dx || dy)
      this.client.input.scroll(dx, dy, e.ctrlKey);
  }
  hovering = false;
  // keys go to the hidden input only while it is useful: a viewer typing in the chat must not lose
  // the focus by brushing the video (touch devices: focusing would pop up the on-screen keyboard)
  focusIfActive() {
    if (this.hovering && this.active && !this.client.isTouchDevice)
      this.input.focus();
  }
  onEnter(e) {
    this.hovering = true;
    this.focusIfActive();
    if (this.client.controlling) {
      this.client.send("keyboard/modifiers", {
        capslock: e.getModifierState("CapsLock"),
        numlock: e.getModifierState("NumLock")
      });
    }
  }
  onDrop(e) {
    if (!this.client.controlling && !this.client.implicitControl)
      return;
    const files = [...e.dataTransfer?.files ?? []];
    if (files.length)
      this.client.uploadDrop({ ...this.pos(e), files });
  }
  /////////////////////////////
  // keyboard
  /////////////////////////////
  onKeyDown(key) {
    if (!this.active) {
      this.noKeyUp.add(key);
      return true;
    }
    if (this.ctrlDown && key === XK.v) {
      this.noKeyUp.add(key);
      return true;
    }
    if (isCtrl(key))
      this.ctrlDown = key;
    this.client.input.key(key, true);
    return isCtrl(key);
  }
  onKeyUp(key) {
    if (this.noKeyUp.delete(key))
      return;
    if (isCtrl(key))
      this.ctrlDown = 0;
    this.client.input.key(key, false);
  }
  // the remote gets its paste keystroke once the clipboard is settled. By then the user may have
  // let go of Ctrl, so the chord is completed with our own Ctrl when needed
  async onPaste(text) {
    if (!this.active)
      return;
    await this.client.preparePaste(text);
    if (!this.active)
      return;
    const ctrl = this.ctrlDown;
    if (!ctrl)
      this.client.input.key(XK.Control_L, true);
    this.client.input.key(XK.v, true);
    this.client.input.key(XK.v, false);
    if (!ctrl)
      this.client.input.key(XK.Control_L, false);
  }
  mobileKeyboardToggle() {
    const open = this.client.state.mobile_keyboard_open;
    if (open)
      this.input.blur();
    else
      this.input.focus();
    this.client.store.setState({ mobile_keyboard_open: !open });
  }
  /////////////////////////////
  // touch
  /////////////////////////////
  onTouch(e) {
    if (!this.client.controlling) {
      if (e.type === "touchstart" && this.client.implicitControl)
        this.client.request();
      return;
    }
    e.preventDefault();
    e.stopPropagation();
    if (this.client.state.control.locked)
      return;
    if (this.client.state.control.touch) {
      for (const t2 of e.changedTouches) {
        const p = this.pos(t2);
        this.client.input.touch(TOUCH_PHASE[e.type], t2.identifier, p.x, p.y, Math.round(t2.force * 255));
      }
      return;
    }
    const t = e.changedTouches[0];
    this.move(this.pos(t));
    if (e.type === "touchstart")
      this.button(1, true);
    else if (e.type !== "touchmove")
      this.button(1, false);
  }
  /////////////////////////////
  // host changes + remote cursor
  /////////////////////////////
  onControl(controlling) {
    this.ctrlDown = 0;
    this.noKeyUp.clear();
    this.updateCursorStyle();
    this.draw();
    const pending = this.pendingPress;
    this.pendingPress = null;
    if (controlling && pending && performance.now() - pending.at < 2e3) {
      const e = pending.e;
      this.move(this.pos(e));
      this.button(e.button + 1, true);
      if (!this.mouseDown)
        this.button(e.button + 1, false);
    }
  }
  onCursorImage(img) {
    const old = this.cursor;
    this.cursor = img;
    this.cursorImg.src = img.uri;
    this.updateCursorStyle();
    if (old)
      URL.revokeObjectURL(old.uri);
  }
  onCursorPosition(p) {
    this.cursorPos = p;
    this.draw();
  }
  clearCursor() {
    this.cursorPos = null;
    this.draw();
  }
  // the host sees the remote cursor as their real mouse pointer
  updateCursorStyle() {
    const c = this.cursor;
    this.input.style.cursor = this.client.controlling && c ? `url(${c.uri}) ${c.x} ${c.y}, default` : "default";
  }
  // devicePixelRatio is read on resize only; listen to matchMedia(resolution) if
  // cursors look blurry after dragging the window to another monitor
  resize() {
    const { width, height } = this.size;
    this.canvas.width = width * devicePixelRatio;
    this.canvas.height = height * devicePixelRatio;
    this.draw();
  }
  draw = () => {
    if (this.drawQueued)
      return;
    this.drawQueued = true;
    requestAnimationFrame(() => {
      this.drawQueued = false;
      this.paint();
    });
  };
  // everyone but the host sees the host's cursor drawn here, tagged with their name
  paint() {
    const { ctx, cursor: cursor2, cursorPos: cursorPos2 } = this;
    const { width, height } = this.size;
    ctx.setTransform(devicePixelRatio, 0, 0, devicePixelRatio, 0, 0);
    ctx.clearRect(0, 0, width, height);
    if (this.client.controlling || !cursor2 || !cursorPos2 || cursor2.width <= 1 && cursor2.height <= 1)
      return;
    const screen = this.client.state.screen.size;
    let x = Math.round(cursorPos2.x / screen.width * width);
    let y = Math.round(cursorPos2.y / screen.height * height);
    ctx.drawImage(this.cursorImg, x - cursor2.x, y - cursor2.y, cursor2.width, cursor2.height);
    const hostId = this.client.state.control.host_id;
    const name = hostId ? this.client.state.sessions[hostId]?.profile.name : "";
    if (!name)
      return;
    x += cursor2.width;
    y += cursor2.height;
    ctx.font = "14px Arial, sans-serif";
    ctx.textBaseline = "top";
    ctx.shadowColor = "black";
    ctx.shadowBlur = 2;
    ctx.lineWidth = 2;
    ctx.fillStyle = "black";
    ctx.strokeText(name, x, y);
    ctx.shadowBlur = 0;
    ctx.fillStyle = "white";
    ctx.fillText(name, x, y);
  }
};

// cursor.mjs
function cursorStyle(cursor2) {
  if (!cursor2 || cursor2.width <= 1 || cursor2.height <= 1) return "none";
  return `url("${cursor2.uri}") ${cursor2.x} ${cursor2.y}, default`;
}
function observerPosition(position, screen, cursor2) {
  return { left: `${100 * position.x / screen.width}%`, top: `${100 * position.y / screen.height}%`, transform: `translate(${-cursor2.x}px, ${-cursor2.y}px)` };
}

// viewer.js
var $ = (id) => document.getElementById(id);
var q = new URLSearchParams(location.search);
var native = q.get("mode") === "native";
var session = q.get("session") || crypto.randomUUID();
var lease = q.get("token") ? { token: q.get("token"), session } : null;
var client;
var overlay;
var sequence = Promise.resolve();
var dropped = 0;
var lastMove = 0;
var detachOverlay = null;
var cursor = null;
var cursorPos = null;
var cursorCleanup = [];
function syncCursor() {
  if (native) {
    $("media").dataset.observer = String(!client?.controlling);
    return;
  }
  const active = !!lease, visible = !!(cursor && cursorPos && cursor.width > 1 && cursor.height > 1);
  $("gated").style.cursor = active && cursor ? cursorStyle(cursor) : visible ? "none" : "default";
  const img = $("remote-cursor");
  img.hidden = active || !visible;
  if (visible) {
    img.src = cursor.uri;
    img.width = cursor.width;
    img.height = cursor.height;
    Object.assign(img.style, observerPosition(cursorPos, client.state.screen.size, cursor));
  }
}
history.replaceState(null, "", location.pathname + (native ? "?mode=native" : ""));
var api = async (p, b) => {
  const r = await fetch(p, { method: b ? "POST" : "GET", headers: b ? { "content-type": "application/json" } : void 0, body: b ? JSON.stringify(b) : void 0 });
  const j = await r.json();
  if (!r.ok) throw new Error(j.error || r.status);
  return j;
};
function error(e) {
  $("status").textContent = e.message;
}
function release(beacon = false) {
  const b = { action: "release", session, token: lease?.token };
  lease = null;
  syncCursor();
  sequence = Promise.resolve();
  if (beacon) {
    navigator.sendBeacon("/control", new Blob([JSON.stringify(b)], { type: "application/json" }));
    return;
  }
  return api("/control", b).catch(error);
}
function queue(events) {
  if (!lease || native) return;
  const auth = { ...lease, session };
  sequence = sequence.catch(() => {
  }).then(() => {
    if (!lease || lease.token !== auth.token) {
      dropped++;
      return;
    }
    return api("/input", { ...auth, events });
  }).catch((e) => {
    lease = null;
    syncCursor();
    error(e);
  });
}
var pos = (e) => {
  const r = $("gated").getBoundingClientRect(), s = client.state.screen.size;
  return { x: Math.max(0, Math.min(s.width - 1, Math.floor((e.clientX - r.left) * s.width / r.width))), y: Math.max(0, Math.min(s.height - 1, Math.floor((e.clientY - r.top) * s.height / r.height))) };
};
async function connect() {
  detachOverlay?.();
  detachOverlay = null;
  overlay = null;
  cursorCleanup.splice(0).forEach((fn) => fn());
  if (cursor) URL.revokeObjectURL(cursor.uri);
  cursor = null;
  cursorPos = null;
  $("remote-cursor").hidden = true;
  client?.disconnect();
  const login = await api("/viewer-session", { session });
  client = new NekoClient({ autoplay: true });
  client.setUrl(location.origin + "/stream");
  await client.authenticate(login.token);
  client.transport.attach($("media"));
  client.mute();
  client.connect();
  $("gated").style.display = native ? "none" : "block";
  if (native) {
    overlay = new Overlay(client);
    detachOverlay = overlay.attach($("media"));
  }
  if (!native) {
    cursorCleanup.push(client.transport.on("cursor.image", (img) => {
      const old = cursor;
      cursor = img;
      syncCursor();
      if (old) URL.revokeObjectURL(old.uri);
    }), client.transport.on("cursor.position", (p) => {
      cursorPos = p;
      syncCursor();
    }));
  }
  cursorCleanup.push(client.store.subscribe((s) => s.control.host_id, syncCursor));
  syncCursor();
  client.store.subscribe((s) => s.screen.size, (s) => {
    $("stage").style.aspectRatio = s.width + "/" + s.height;
    fitScreen();
  });
  client.events.on("connection.status", (status) => $("status").textContent = "Stream " + status);
  client.events.on("connection.closed", (e) => {
    lease = null;
    cursorPos = null;
    syncCursor();
    if (e) error(e);
  });
  window.__sidekicksNeko = {
    get client() {
      return client;
    },
    get session() {
      return session;
    },
    get lease() {
      return lease;
    },
    native,
    queue,
    api,
    get dropped() {
      return dropped;
    },
    async take() {
      return take();
    },
    release,
    reconnect: connect,
    get cursor() {
      return cursor ? { width: cursor.width, height: cursor.height, x: cursor.x, y: cursor.y, mode: lease ? "local-css" : "remote-observer" } : null;
    },
    async stats() {
      const pc = client.transport.pc;
      if (!pc) return [];
      return [...await pc.getStats()].map((x) => x[1]);
    },
    framePixel(x, y) {
      const v = client.transport.element, c = document.createElement("canvas");
      c.width = v.videoWidth;
      c.height = v.videoHeight;
      const ctx = c.getContext("2d");
      ctx.drawImage(v, 0, 0);
      return [...ctx.getImageData(x, y, 1, 1).data];
    }
  };
}
async function take() {
  $("status").textContent = "Taking control \xB7 releasing prior held input\u2026";
  lease = await api("/control", { action: "acquire", owner: "human", session, token: lease?.token, leaseMs: 3e4, mode: native ? "native" : "gated" });
  syncCursor();
  if (!native) $("gated").focus();
  $("status").textContent = "Human control ready \xB7 " + (native ? "native data channel" : "gated HTTP");
  return { ...lease, session };
}
function launcher(key) {
  if (!lease) {
    error(new Error("Take human control first"));
    return;
  }
  const terminal = key === "Enter", modifier = terminal ? 65515 : 65513, target = terminal ? 65293 : 65478;
  if (native) {
    if (!client.controlling) {
      error(new Error("Native control is not ready"));
      return;
    }
    client.input.key(modifier, true);
    client.input.key(target, true);
    client.input.key(target, false);
    client.input.key(modifier, false);
    overlay.wrap.querySelector("textarea").focus();
  } else {
    const mod = terminal ? "Meta" : "Alt", target2 = terminal ? "Enter" : "F9";
    queue([{ type: "key", key: mod, action: "down" }, { type: "key", key: target2, action: "down" }, { type: "key", key: target2, action: "up" }, { type: "key", key: mod, action: "up" }]);
    $("gated").focus();
  }
}
$("terminal").onclick = () => launcher("Enter");
$("minimize").onclick = () => launcher("m");
$("take").onclick = () => take().catch(error);
$("release").onclick = () => release();
$("stop").onclick = async () => {
  lease = null;
  syncCursor();
  await api("/control", { action: "stop" }).catch(error);
};
$("reconnect").onclick = () => connect().catch(error);
$("sound").onclick = () => {
  client.unmute();
  client.play();
};
$("mode").textContent = native ? "Native Neko / data-channel diagnostic" : "Workbench / lease-gated HTTP input";
$("switch").textContent = native ? "Lease-gated workbench input" : "Native input diagnostic";
$("switch").href = native ? "/" : "/?mode=native";
$("help").textContent = native ? "Human-only diagnostic. Official Neko overlay sends native WebRTC data-channel input after the server transfers host ownership. Bot HTTP input is blocked for this lease." : "Human input and normalized automation both use the adapter HTTP lease gate, then Neko\u2019s v3 control websocket. The stream remains WebRTC. Take control before typing.";
var layer = $("gated");
layer.addEventListener("contextmenu", (e) => e.preventDefault());
layer.addEventListener("pointerdown", (e) => {
  if (!lease) return;
  e.preventDefault();
  layer.focus();
  layer.setPointerCapture(e.pointerId);
  queue([{ type: "pointer", ...pos(e), action: "down", button: ["left", "middle", "right"][e.button] }]);
});
layer.addEventListener("pointermove", (e) => {
  if (!lease || performance.now() - lastMove < 16) return;
  lastMove = performance.now();
  queue([{ type: "pointer", ...pos(e), action: "move" }]);
});
layer.addEventListener("pointerup", (e) => {
  e.preventDefault();
  queue([{ type: "pointer", ...pos(e), action: "up", button: ["left", "middle", "right"][e.button] }]);
});
layer.addEventListener("pointercancel", () => queue([{ type: "releaseAll" }]));
layer.addEventListener("wheel", (e) => {
  e.preventDefault();
  queue([{ type: "scroll", ...pos(e), deltaX: Math.round(e.deltaX), deltaY: Math.round(e.deltaY) }]);
}, { passive: false });
for (const name of ["keydown", "keyup"]) layer.addEventListener(name, (e) => {
  if (!lease) return;
  e.preventDefault();
  if (!e.repeat) queue([{ type: "key", key: e.key === " " ? "Space" : e.key, action: name === "keydown" ? "down" : "up" }]);
});
window.addEventListener("blur", () => {
  if (lease) release(true);
});
window.addEventListener("pagehide", () => {
  if (lease) release(true);
});
document.addEventListener("visibilitychange", () => {
  if (document.hidden && lease) release(true);
});
setInterval(async () => {
  try {
    const c = await api("/control");
    if (lease && c.session !== session) lease = null;
    else if (lease) lease = await api("/control", { action: "acquire", owner: "human", session, token: lease?.token, leaseMs: 3e4, mode: native ? "native" : "gated" });
    syncCursor();
    const v = client?.transport.element;
    $("metrics").textContent = JSON.stringify({ owner: c.owner ?? "none", mode: c.mode ?? "none", decodedVideo: v ? `${v.videoWidth}\xD7${v.videoHeight}` : "pending", currentTime: v?.currentTime, playback: v?.getVideoPlaybackQuality?.(), queuedInputDropped: dropped }, null, 2);
  } catch (e) {
    error(e);
  }
}, 3e3);
connect().catch(error);
function fitScreen() {
  const s = client?.state.screen.size;
  if (!s?.width) return;
  const stage = $("stage"), w = Math.min(stage.clientWidth, stage.clientHeight * s.width / s.height), h = w * s.height / s.width, x = (stage.clientWidth - w) / 2, y = (stage.clientHeight - h) / 2;
  Object.assign($("media").style, { width: w + "px", height: h + "px", marginLeft: x + "px", marginTop: y + "px" });
  Object.assign($("gated").style, { width: w + "px", height: h + "px", left: x + "px", top: y + "px", right: "auto", bottom: "auto" });
  syncCursor();
}
new ResizeObserver(fitScreen).observe($("stage"));
async function toggleFullscreen() {
  try {
    if (document.fullscreenElement) await document.exitFullscreen();
    else await $("stage").requestFullscreen();
  } catch (e) {
    error(e);
  }
}
$("fullscreen").onclick = toggleFullscreen;
$("exit-fullscreen").onclick = toggleFullscreen;
window.addEventListener("keydown", (e) => {
  if (e.key === "Escape" && document.fullscreenElement) {
    e.preventDefault();
    e.stopImmediatePropagation();
    document.exitFullscreen().catch(error);
  }
}, { capture: true });
document.addEventListener("fullscreenchange", async () => {
  const restore = !!lease;
  $("fullscreen").textContent = document.fullscreenElement ? "Exit viewer fullscreen" : "Viewer fullscreen";
  fitScreen();
  if (restore) {
    await release();
    if (native && overlay) detachOverlay = overlay.attach($("media"));
    await take().catch(error);
  }
});
