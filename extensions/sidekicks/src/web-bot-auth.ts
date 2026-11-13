import type { Protocol } from "devtools-protocol";

const nativeHost = "com.sidekicks.wba";
const signatureHeaders = ["Signature", "Signature-Input", "Signature-Agent"] as const;
const reservedHeaders = new Set<string>(signatureHeaders.map((name) => name.toLowerCase()));
const maxBrokerRetryDelay = 2000;
const brokerRetryDelays = [250, 500, 1000, maxBrokerRetryDelay, maxBrokerRetryDelay];

interface TabState {
  rootFrameId?: Protocol.Page.FrameId;
  origins?: Set<string>;
  targetId?: string;
}

type BrokerRequest =
  | { type: "configure" }
  | {
      type: "ready";
      targetId: string;
    }
  | {
      type: "retire";
      targetId: string;
    }
  | {
      type: "sign";
      method: "GET" | "HEAD";
      url: string;
    };

interface BrokerResponse {
  id?: string;
  ok?: boolean;
  origins?: unknown;
  headers?: Record<string, unknown>;
  leaseMs?: unknown;
}

const tabs = new Map<number, TabState>();
const retiredTabs = new Set<number>();

async function broker(request: BrokerRequest): Promise<BrokerResponse> {
  const id = crypto.randomUUID();
  const response = await chrome.runtime.sendNativeMessage(nativeHost, {
    id,
    ...request,
    ...(request.type === "ready" ? { sentAtMs: Date.now() } : {}),
  }) as
    BrokerResponse | undefined;
  if (response?.id !== id || response.ok !== true) {
    throw new Error("Signing Broker request failed");
  }
  return response;
}

async function retryBroker(
  request: BrokerRequest,
  stillNeeded?: () => boolean | Promise<boolean>,
): Promise<BrokerResponse | undefined> {
  let retries = 0;
  while (!stillNeeded || await stillNeeded()) {
    try {
      return await broker(request);
    } catch (error) {
      const delay = brokerRetryDelays[retries++];
      if (delay === undefined && request.type !== "configure") throw error;
      await new Promise((resolve) => setTimeout(resolve, delay ?? maxBrokerRetryDelay));
    }
  }
}

function blankTab(tab: chrome.tabs.Tab): boolean {
  return !tab.incognito && (tab.pendingUrl || tab.url) === "about:blank";
}

function retireTab(tabId: number): void {
  const state = tabs.get(tabId);
  tabs.delete(tabId);
  if (state?.targetId) {
    // A prepared tab must be replaced after losing its debugger attachment.
    retiredTabs.add(tabId);
    void retryBroker({
      type: "retire",
      targetId: state.targetId,
    })
      .catch(() => console.warn("Sidekicks WBA could not retire a tab"));
  }
}

async function maintainReadiness(tabId: number, state: TabState, targetId: string): Promise<void> {
  const stillAttached = () => tabs.get(tabId) === state;
  while (stillAttached()) {
    const response = await retryBroker({
      type: "ready",
      targetId,
    }, stillAttached);
    if (!response || !stillAttached()) return;
    const { leaseMs } = response;
    if (typeof leaseMs !== "number" || !Number.isSafeInteger(leaseMs) ||
      leaseMs < 3 || leaseMs > 2_147_483_647) {
      throw new Error("Invalid readiness lease");
    }
    await new Promise((resolve) => setTimeout(resolve, Math.floor(leaseMs / 3)));
  }
}

async function attach(tab: chrome.tabs.Tab): Promise<void> {
  const tabId = tab.id;
  if (tabId == null || tabs.has(tabId) || retiredTabs.has(tabId)) return;
  // The launcher opens a blank tab and waits for readiness before navigating.
  if (!blankTab(tab)) return;

  const target = { tabId };
  const state: TabState = {};
  tabs.set(tabId, state);
  const stillBlank = async () => {
    const current = await chrome.tabs.get(tabId).catch(() => undefined);
    return tabs.get(tabId) === state && current !== undefined && blankTab(current);
  };
  let attached = false;
  try {
    const config = await retryBroker({ type: "configure" }, stillBlank);
    if (!config || !(await stillBlank())) {
      if (tabs.get(tabId) === state) tabs.delete(tabId);
      return;
    }
    if (!Array.isArray(config.origins)) throw new Error("Missing signing origins");
    const origins = new Set(config.origins.map((origin: unknown) => {
      if (typeof origin !== "string") throw new Error("Invalid signing origin");
      const url = new URL(origin);
      if (url.protocol !== "https:" || url.origin !== origin) {
        throw new Error("Invalid signing origin");
      }
      return origin;
    }));
    if (origins.size === 0) {
      tabs.delete(tabId);
      return;
    }

    await chrome.debugger.attach(target, "1.3");
    attached = true;
    const tree = await chrome.debugger.sendCommand(target, "Page.getFrameTree") as
      Protocol.Page.GetFrameTreeResponse | undefined;
    if (!tree?.frameTree.frame.id) throw new Error("Missing main frame");
    state.rootFrameId = tree.frameTree.frame.id;
    state.origins = origins;
    await chrome.debugger.sendCommand(target, "Fetch.enable", {
      patterns: [...origins].map((origin) => ({
        urlPattern: origin + "/*",
        resourceType: "Document",
        requestStage: "Request",
      })),
    });
    const debuggee = (await chrome.debugger.getTargets()).find((entry) => entry.tabId === tabId);
    if (tabs.get(tabId) !== state) return;
    if (!debuggee) throw new Error("Missing browser target");
    state.targetId = debuggee.id;
    await maintainReadiness(tabId, state, debuggee.id);
  } catch {
    if (tabs.get(tabId) === state) {
      retireTab(tabId);
      if (attached) await chrome.debugger.detach(target).catch(() => {});
    }
    console.warn("Sidekicks WBA could not prepare a tab");
  }
}

async function resume(target: chrome.debugger.Debuggee, event: Protocol.Fetch.RequestPausedEvent): Promise<void> {
  if (target.tabId == null) return;
  const { requestId, request, frameId } = event;
  const { method } = request;
  const state = tabs.get(target.tabId);
  try {
    const url = new URL(request.url);
    const eligible = state?.origins?.has(url.origin) && frameId === state.rootFrameId &&
      (method === "GET" || method === "HEAD");
    if (!eligible) {
      await chrome.debugger.sendCommand(target, "Fetch.continueRequest", { requestId });
      return;
    }

    const original = Object.entries(request.headers);
    if (original.some(([name]) => reservedHeaders.has(name.toLowerCase()))) {
      throw new Error("Conflicting request signature");
    }
    url.hash = "";
    const { headers } = await broker({
      type: "sign",
      method,
      url: url.href,
    });
    const additions = signatureHeaders.map((name) => {
      const value = headers?.[name];
      if (typeof value !== "string" || value.length === 0 || /[\r\n]/.test(value)) {
        throw new Error("Invalid signature header");
      }
      return [name, value];
    });
    if (tabs.get(target.tabId) !== state) return;
    await chrome.debugger.sendCommand(target, "Fetch.continueRequest", {
      requestId,
      headers: [...original, ...additions].map(([name, value]) => ({
        name,
        value,
      })),
    });
  } catch {
    await chrome.debugger.sendCommand(target, "Fetch.failRequest", {
      requestId,
      errorReason: "BlockedByClient",
    });
  }
}

export function startWebBotAuth(): void {
  chrome.debugger.onEvent.addListener((target, method, event) => {
    if (method === "Fetch.requestPaused" && event) {
      void resume(target, event as unknown as Protocol.Fetch.RequestPausedEvent)
        .catch(() => console.warn("Sidekicks WBA could not resolve a paused request"));
    }
  });
  chrome.debugger.onDetach.addListener(({ tabId }) => {
    if (tabId != null) retireTab(tabId);
  });
  chrome.tabs.onRemoved.addListener((tabId) => {
    retireTab(tabId);
    retiredTabs.delete(tabId);
  });
  chrome.tabs.onCreated.addListener((tab) => void attach(tab));
  chrome.tabs.onUpdated.addListener((_tabId, _change, tab) => void attach(tab));
  void chrome.tabs.query({}).then((openTabs) => openTabs.forEach((tab) => void attach(tab)));
}
