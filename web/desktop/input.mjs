// Sidekicks-owned input: all writes use the authenticated, epoch-gated Go X11
// endpoint. Protocol viewers remain receive-only. No upstream native input SDK.
const named = new Set([
  "Enter",
  "Control",
  "Shift",
  "Alt",
  "Meta",
  "ArrowUp",
  "ArrowDown",
  "ArrowLeft",
  "ArrowRight",
  "Escape",
  "Backspace",
  "Tab",
  "Delete",
  "Home",
  "End",
  "PageUp",
  "PageDown",
]);
const physical = {
  Space: " ",
  Minus: "-",
  Equal: "=",
  Backquote: "`",
  BracketLeft: "[",
  BracketRight: "]",
  Backslash: "\\",
  Semicolon: ";",
  Quote: "'",
  Comma: ",",
  Period: ".",
  Slash: "/",
  NumpadEnter: "Enter",
  NumpadAdd: "+",
  NumpadSubtract: "-",
  NumpadDivide: "/",
  NumpadDecimal: ".",
};
const modifiers = [
  ["Shift", "shiftKey"],
  ["Control", "ctrlKey"],
  ["Alt", "altKey"],
  ["Meta", "metaKey"],
];
export function normalizedKey(event) {
  if (named.has(event.key) || /^F([1-9]|1[0-2])$/.test(event.key))
    return event.key;
  if (/^Key[A-Z]$/.test(event.code)) return event.code.slice(3).toLowerCase();
  if (/^Digit[0-9]$/.test(event.code)) return event.code.slice(5);
  if (/^Numpad[0-9]$/.test(event.code)) return event.code.slice(6);
  return physical[event.code] || null;
}
export function mapPoint(rect, width, height, clientX, clientY) {
  if (
    ![
      rect?.left,
      rect?.top,
      rect?.width,
      rect?.height,
      width,
      height,
      clientX,
      clientY,
    ].every(Number.isFinite) ||
    rect.width <= 0 ||
    rect.height <= 0 ||
    width < 1 ||
    height < 1
  )
    return null;
  return {
    x: Math.max(
      0,
      Math.min(
        width - 1,
        Math.round(((clientX - rect.left) * width) / rect.width),
      ),
    ),
    y: Math.max(
      0,
      Math.min(
        height - 1,
        Math.round(((clientY - rect.top) * height) / rect.height),
      ),
    ),
  };
}
export function streamGeometry(element) {
  if (!element) return null;
  const width = element.videoWidth ?? element.width,
    height = element.videoHeight ?? element.height;
  const box = element.getBoundingClientRect();
  if (!width || !height || !box.width || !box.height) return null;
  const scale = Math.min(box.width / width, box.height / height);
  return {
    element,
    width,
    height,
    rect: {
      left: box.left + (box.width - width * scale) / 2,
      top: box.top + (box.height - height * scale) / 2,
      width: width * scale,
      height: height * scale,
    },
  };
}
export class DesktopInput {
  constructor({ container, geometry, send, getEpoch, onFailure }) {
    Object.assign(this, { container, geometry, send, getEpoch, onFailure });
    this.events = [];
    this.active = 0;
    this.generation = 0;
    this.running = false;
    this.pressed = new Map();
    this.mods = new Map();
    this.buttons = new Set();
    this.listeners = [];
    this.box = document.createElement("div");
    this.box.className = "sidekicks-input";
    this.box.tabIndex = 0;
    this.box.setAttribute("role", "application");
    this.box.setAttribute("aria-label", "Remote desktop keyboard and pointer");
    Object.assign(this.box.style, {
      position: "absolute",
      zIndex: "5",
      background: "transparent",
      outline: "none",
      pointerEvents: "none",
      touchAction: "none",
      cursor: "default",
    });
    this.cursor = document.createElement("img");
    Object.assign(this.cursor.style, {
      position: "absolute",
      zIndex: "6",
      pointerEvents: "none",
      display: "none",
    });
    const on = (element, type, fn, options) => {
      element.addEventListener(type, fn, options);
      this.listeners.push(() => element.removeEventListener(type, fn, options));
    };
    const stop = (event) => {
      event.preventDefault();
      event.stopImmediatePropagation();
    };
    on(this.box, "pointerdown", (event) => {
      if (!this.live()) return;
      stop(event);
      this.box.focus({ preventScroll: true });
      const button = ["left", "middle", "right"][event.button],
        p = this.point(event);
      if (!button || !p) return;
      this.syncModifiers(event);
      this.buttons.add(button);
      this.box.setPointerCapture(event.pointerId);
      this.enqueue({ type: "pointer", action: "down", button, ...p });
    });
    on(this.box, "pointermove", (event) => {
      if (!this.live()) return;
      stop(event);
      const p = this.point(event);
      if (p) this.enqueue({ type: "pointer", action: "move", ...p });
    });
    on(this.box, "pointerup", (event) => {
      if (!this.live()) return;
      stop(event);
      const button = ["left", "middle", "right"][event.button],
        p = this.point(event);
      if (button && p) {
        this.syncModifiers(event);
        this.buttons.delete(button);
        this.enqueue({ type: "pointer", action: "up", button, ...p });
      }
      if (this.box.hasPointerCapture(event.pointerId))
        this.box.releasePointerCapture(event.pointerId);
    });
    on(this.box, "pointercancel", () => this.releaseAll());
    on(this.box, "lostpointercapture", () => {
      if (this.buttons.size) this.releaseAll();
    });
    on(this.box, "contextmenu", stop);
    on(
      this.box,
      "wheel",
      (event) => {
        if (!this.live()) return;
        stop(event);
        const p = this.point(event);
        if (!p) return;
        this.syncModifiers(event);
        const factor =
          event.deltaMode === 1
            ? 19
            : event.deltaMode === 2
              ? this.geometry()?.height || 720
              : 1;
        const bounded = (value) =>
          Math.max(-2400, Math.min(2400, Math.round(value * factor)));
        const deltaX = bounded(event.deltaX),
          deltaY = bounded(event.deltaY);
        if (deltaX || deltaY)
          this.enqueue({ type: "scroll", ...p, deltaX, deltaY });
      },
      { passive: false },
    );
    on(this.box, "keydown", (event) => {
      if (!this.live()) return;
      stop(event);
      if (
        event.getModifierState?.("CapsLock") ||
        event.isComposing ||
        event.key === "Dead" ||
        event.key === "Process" ||
        event.getModifierState?.("AltGraph")
      ) {
        this.fail(
          Error(
            "Caps Lock, IME, dead keys and AltGraph are unsupported by gated input",
          ),
        );
        return;
      }
      const key = normalizedKey(event);
      if (!key) {
        this.fail(Error("Key is unsupported by the US gated input mapping"));
        return;
      }
      this.syncModifiers(event);
      if (modifiers.some(([name]) => name === key)) return;
      this.pressed.set(event.code, key);
      this.enqueue({ type: "key", key, action: "down" });
    });
    on(
      window,
      "keyup",
      (event) => {
        if (
          !this.live() ||
          (!this.pressed.has(event.code) &&
            !this.mods.size &&
            document.activeElement !== this.box)
        )
          return;
        stop(event);
        this.syncModifiers(event);
        const key = this.pressed.get(event.code);
        this.pressed.delete(event.code);
        if (key) this.enqueue({ type: "key", key, action: "up" });
      },
      { capture: true },
    );
    on(this.box, "blur", () => this.releaseAll());
    on(this.box, "paste", (event) => {
      stop(event);
      this.fail(
        Error(
          "Host clipboard transfer is unsupported; guest-local Ctrl+V remains available",
        ),
      );
    });
    on(window, "resize", () => this.sync());
    on(document, "fullscreenchange", () => this.sync());
    this.observer = new ResizeObserver(() => this.sync());
    this.observer.observe(container);
    this.sync();
  }
  live() {
    return this.active > 0 && this.active === this.getEpoch();
  }
  point(event) {
    const g = this.geometry();
    return g
      ? mapPoint(g.rect, g.width, g.height, event.clientX, event.clientY)
      : null;
  }
  syncModifiers(event) {
    for (const [key, flag] of modifiers) {
      const down = !!event[flag],
        was = this.mods.has(key);
      if (down === was) continue;
      if (down) this.mods.set(key, true);
      else this.mods.delete(key);
      this.enqueue({ type: "key", key, action: down ? "down" : "up" });
    }
  }
  enqueue(event) {
    if (!this.live()) return;
    const last = this.events[this.events.length - 1];
    if (
      event.type === "pointer" &&
      event.action === "move" &&
      last?.event.type === "pointer" &&
      last.event.action === "move"
    )
      last.event = event;
    else {
      if (this.events.length >= 256) {
        this.fail(Error("Gated input queue limit reached"));
        return;
      }
      this.events.push({ event, epoch: this.active });
    }
    this.drain();
  }
  async drain() {
    if (this.running) return;
    this.running = true;
    try {
      while (this.events.length && this.live()) {
        const epoch = this.active,
          generation = this.generation;
        const batch = this.events
          .splice(0, 32)
          .filter((item) => item.epoch === epoch)
          .map((item) => item.event);
        if (!batch.length) continue;
        try {
          // Never cancel a checked X11 write merely because the UI released.
          // The server gate revokes/serializes it before acknowledging reset.
          await this.send(batch, epoch);
        } catch (e) {
          if (generation === this.generation) this.fail(e);
          break;
        }
      }
    } finally {
      this.running = false;
      if (this.events.length && this.live()) this.drain();
    }
  }
  activate(epoch) {
    if (this.active !== epoch) this.suspend();
    this.active = epoch;
    this.sync();
    this.box.focus({ preventScroll: true });
  }
  suspend() {
    this.generation++;
    this.active = 0;
    this.events = [];
    this.pressed.clear();
    this.mods.clear();
    this.buttons.clear();
    this.sync();
  }
  releaseAll() {
    if (!this.live()) return;
    this.pressed.clear();
    this.mods.clear();
    this.buttons.clear();
    this.enqueue({ type: "releaseAll" });
  }
  fail(error) {
    const epoch = this.active;
    this.suspend();
    if (epoch) this.onFailure(error, epoch);
  }
  sync() {
    const g = this.geometry(),
      parent = this.container.getBoundingClientRect();
    if (g?.element && g.element !== this.observedSource) {
      if (this.observedSource) this.observer.unobserve(this.observedSource);
      this.observedSource = g.element;
      this.observer.observe(g.element);
    }
    if (this.box.parentNode !== this.container)
      this.container.append(this.box, this.cursor);
    this.box.style.pointerEvents = this.live() ? "auto" : "none";
    if (!g) {
      this.box.style.display = "none";
      this.cursor.style.display = "none";
      return;
    }
    this.box.style.display = "block";
    Object.assign(this.box.style, {
      left: g.rect.left - parent.left + "px",
      top: g.rect.top - parent.top + "px",
      width: g.rect.width + "px",
      height: g.rect.height + "px",
    });
    if (this.cursorImage)
      this.box.style.cursor = `url("${this.cursorImage.uri}") ${this.cursorImage.x} ${this.cursorImage.y}, default`;
    else if (g.element && typeof getComputedStyle === "function") {
      const current = getComputedStyle(g.element).cursor;
      this.box.style.cursor =
        current && current !== "auto" ? current : "default";
    }
    this.cursor.style.display =
      !this.live() && this.cursorImage && this.cursorPosition
        ? "block"
        : "none";
    if (this.cursorPosition && this.cursorImage) {
      const sx = g.rect.width / g.width,
        sy = g.rect.height / g.height;
      Object.assign(this.cursor.style, {
        left:
          g.rect.left -
          parent.left +
          (this.cursorPosition.x - this.cursorImage.x) * sx +
          "px",
        top:
          g.rect.top -
          parent.top +
          (this.cursorPosition.y - this.cursorImage.y) * sy +
          "px",
        width: this.cursorImage.width * sx + "px",
        height: this.cursorImage.height * sy + "px",
      });
    }
  }
  setCursor(image) {
    if (
      !image ||
      ![image.width, image.height, image.x, image.y].every(Number.isInteger) ||
      image.width < 1 ||
      image.height < 1 ||
      image.width > 128 ||
      image.height > 128 ||
      image.x < 0 ||
      image.y < 0 ||
      image.x >= image.width ||
      image.y >= image.height ||
      typeof image.uri !== "string" ||
      image.uri.length > 200000 ||
      !(
        image.uri.startsWith("blob:" + location.origin + "/") ||
        image.uri.startsWith("data:image/png;base64,")
      )
    )
      return;
    const old = this.cursorImage;
    this.cursorImage = image;
    this.cursor.src = image.uri;
    if (old?.uri.startsWith("blob:")) URL.revokeObjectURL(old.uri);
    this.sync();
  }
  setCursorPosition(position) {
    this.cursorPosition =
      position && Number.isFinite(position.x) && Number.isFinite(position.y)
        ? position
        : null;
    this.sync();
  }
  destroy() {
    this.suspend();
    this.listeners.forEach((fn) => fn());
    this.observer.disconnect();
    this.box.remove();
    this.cursor.remove();
    if (this.cursorImage?.uri.startsWith("blob:"))
      URL.revokeObjectURL(this.cursorImage.uri);
  }
}
