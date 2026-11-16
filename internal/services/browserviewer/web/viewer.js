"use strict";

const screen = document.querySelector("#screen");
const image = document.querySelector("#frame");
const status = document.querySelector("#status");
const waiting = document.querySelector("#waiting");
const token = window.location.hash.slice(1);

if (/^[A-Z2-7]{26,128}$/.test(token)) {
  connect(token);
} else {
  status.textContent = "Viewer link required";
  waiting.textContent = "Open the URL printed by sidekicks browser view.";
}

function connect(credential) {
  const address = new URL("/stream", window.location.href);
  address.protocol = "ws:";
  const socket = new WebSocket(address, ["sidekicks-viewer", credential]);
  socket.addEventListener("message", ({ data }) => {
    const frame = JSON.parse(data);
    image.src = "data:image/jpeg;base64," + frame.jpeg;
    image.hidden = false;
    waiting.hidden = true;
    status.textContent = "Live";
  });
  socket.addEventListener("close", ({ reason }) => {
    status.textContent = reason || "Viewer disconnected";
    waiting.textContent = "Run sidekicks browser view to open a new session.";
  });
  registerInput(socket);
}

function modifiers(event) {
  return (event.altKey ? 1 : 0) | (event.ctrlKey ? 2 : 0) |
    (event.metaKey ? 4 : 0) | (event.shiftKey ? 8 : 0);
}

function position(event) {
  const rect = image.getBoundingClientRect();
  return {
    x: Math.max(0, Math.min(1, (event.clientX - rect.left) / rect.width)),
    y: Math.max(0, Math.min(1, (event.clientY - rect.top) / rect.height))
  };
}

function registerInput(socket) {
  const buttons = ["left", "middle", "right", "back", "forward"];
  let move;
  let scheduledMove = false;

  function send(event) {
    if (socket.readyState === WebSocket.OPEN && image.naturalWidth > 0) {
      socket.send(JSON.stringify(event));
    }
  }

  function pointer(event, type) {
    return {
      type,
      ...position(event),
      button: buttons[event.button],
      buttons: event.buttons,
      clickCount: type === "pointerMove" ? 0 : Math.max(1, Math.min(event.detail, 3)),
      modifiers: modifiers(event)
    };
  }

  image.addEventListener("pointerdown", (event) => {
    event.preventDefault();
    move = undefined;
    screen.focus({ preventScroll: true });
    image.setPointerCapture(event.pointerId);
    send(pointer(event, "pointerDown"));
  });
  image.addEventListener("pointerup", (event) => {
    move = undefined;
    send(pointer(event, "pointerUp"));
  });
  image.addEventListener("pointercancel", (event) => {
    move = undefined;
    for (const button of buttons) {
      send({ ...pointer(event, "pointerUp"), button, buttons: 0 });
    }
  });
  image.addEventListener("pointermove", (event) => {
    move = pointer(event, "pointerMove");
    if (!scheduledMove) {
      scheduledMove = true;
      window.requestAnimationFrame(() => {
        if (move && socket.bufferedAmount < 4096) {
          send(move);
        }
        scheduledMove = false;
        move = undefined;
      });
    }
  });
  image.addEventListener("contextmenu", (event) => event.preventDefault());
  image.addEventListener("wheel", (event) => {
    event.preventDefault();
    const rect = image.getBoundingClientRect();
    const unit = event.deltaMode === 1 ? 16 : event.deltaMode === 2 ? rect.height : 1;
    const scale = unit * image.naturalWidth / rect.width;
    const delta = (value) => Math.max(-10000, Math.min(10000, value * scale));
    send({
      type: "wheel",
      ...position(event),
      deltaX: delta(event.deltaX),
      deltaY: delta(event.deltaY),
      modifiers: modifiers(event)
    });
  }, { passive: false });

  for (const [eventName, type] of [["keydown", "keyDown"], ["keyup", "keyUp"]]) {
    screen.addEventListener(eventName, (event) => {
      if (event.isComposing || ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "v")) {
        return;
      }
      if (socket.readyState !== WebSocket.OPEN || image.hidden) {
        return;
      }
      event.preventDefault();
      const printable = type === "keyDown" && !event.ctrlKey && !event.metaKey &&
        !event.altKey && Array.from(event.key).length === 1;
      send({
        type,
        key: event.key,
        code: event.code,
        keyCode: event.keyCode,
        modifiers: modifiers(event),
        repeat: event.repeat,
        text: printable ? event.key : ""
      });
    });
  }
  screen.addEventListener("paste", (event) => {
    const text = event.clipboardData && event.clipboardData.getData("text/plain");
    if (text) {
      event.preventDefault();
      const characters = Array.from(text);
      for (let start = 0; start < characters.length; start += 2048) {
        send({ type: "text", text: characters.slice(start, start + 2048).join("") });
      }
    }
  });
}
