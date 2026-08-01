const PANEL_ADDRESSES_KEY = "flux-panel-addresses";

export interface PanelAddress {
  name: string;
  address: string;
  inx: boolean;
}

function readPanelAddresses(): PanelAddress[] {
  try {
    const value = window.localStorage.getItem(PANEL_ADDRESSES_KEY);
    if (!value) return [];
    const parsed = JSON.parse(value) as unknown;
    if (!Array.isArray(parsed)) return [];
    return parsed.filter(
      (item): item is PanelAddress =>
        typeof item === "object" &&
        item !== null &&
        typeof (item as PanelAddress).name === "string" &&
        typeof (item as PanelAddress).address === "string" &&
        typeof (item as PanelAddress).inx === "boolean",
    );
  } catch {
    return [];
  }
}

function writePanelAddresses(addresses: PanelAddress[]): void {
  window.localStorage.setItem(PANEL_ADDRESSES_KEY, JSON.stringify(addresses));
}

export function getBrowserPanelAddresses(): PanelAddress[] {
  return readPanelAddresses();
}

export function getCurrentBrowserPanelAddress(): string | null {
  return readPanelAddresses().find((item) => item.inx)?.address || null;
}

export async function getPanelAddresses(
  callback = "setPanelAddresses",
): Promise<void> {
  const bridge = (window as Window & { JsInterface?: Record<string, unknown> })
    .JsInterface;
  if (bridge && typeof bridge.getPanelAddresses === "function") {
    bridge.getPanelAddresses(callback);
    return;
  }
  const webkit = (
    window as Window & {
      webkit?: {
        messageHandlers?: Record<
          string,
          { postMessage: (value: unknown) => void }
        >;
      };
    }
  ).webkit;
  if (webkit?.messageHandlers?.getPanelAddresses) {
    webkit.messageHandlers.getPanelAddresses.postMessage(callback);
    return;
  }
  const setter = (window as unknown as Record<string, unknown>)[callback];
  if (typeof setter === "function") {
    (setter as (addresses: PanelAddress[]) => void)(readPanelAddresses());
  }
}

export async function savePanelAddress(
  name: string,
  address: string,
): Promise<void> {
  const bridge = (window as Window & { JsInterface?: Record<string, unknown> })
    .JsInterface;
  if (bridge && typeof bridge.savePanelAddress === "function") {
    bridge.savePanelAddress(name, address);
    return;
  }
  const webkit = (
    window as Window & {
      webkit?: {
        messageHandlers?: Record<
          string,
          { postMessage: (value: unknown) => void }
        >;
      };
    }
  ).webkit;
  if (webkit?.messageHandlers?.savePanelAddress) {
    webkit.messageHandlers.savePanelAddress.postMessage({ name, address });
    return;
  }
  const addresses = readPanelAddresses();
  const existing = addresses.find((item) => item.name === name);
  const next = addresses.filter((item) => item.name !== name);
  next.push({ name, address, inx: existing?.inx ?? next.length === 0 });
  writePanelAddresses(next);
}

export async function setCurrentPanelAddress(name: string): Promise<void> {
  const bridge = (window as Window & { JsInterface?: Record<string, unknown> })
    .JsInterface;
  if (bridge && typeof bridge.setCurrentPanelAddress === "function") {
    bridge.setCurrentPanelAddress(name);
    return;
  }
  const webkit = (
    window as Window & {
      webkit?: {
        messageHandlers?: Record<
          string,
          { postMessage: (value: unknown) => void }
        >;
      };
    }
  ).webkit;
  if (webkit?.messageHandlers?.setCurrentPanelAddress) {
    webkit.messageHandlers.setCurrentPanelAddress.postMessage({ name });
    return;
  }
  writePanelAddresses(
    readPanelAddresses().map((item) => ({ ...item, inx: item.name === name })),
  );
}

export async function deletePanelAddress(name: string): Promise<void> {
  const bridge = (window as Window & { JsInterface?: Record<string, unknown> })
    .JsInterface;
  if (bridge && typeof bridge.deletePanelAddress === "function") {
    bridge.deletePanelAddress(name);
    return;
  }
  const webkit = (
    window as Window & {
      webkit?: {
        messageHandlers?: Record<
          string,
          { postMessage: (value: unknown) => void }
        >;
      };
    }
  ).webkit;
  if (webkit?.messageHandlers?.deletePanelAddress) {
    webkit.messageHandlers.deletePanelAddress.postMessage({ name });
    return;
  }
  const remaining = readPanelAddresses().filter((item) => item.name !== name);
  if (remaining.length > 0 && !remaining.some((item) => item.inx)) {
    remaining[0].inx = true;
  }
  writePanelAddresses(remaining);
}

export function isWebViewFunc(): boolean {
  const windowWithBridge = window as Window & {
    JsInterface?: { getPanelAddresses?: unknown };
    webkit?: { messageHandlers?: { getPanelAddresses?: unknown } };
  };
  return (
    typeof windowWithBridge.JsInterface?.getPanelAddresses === "function" ||
    typeof windowWithBridge.webkit?.messageHandlers?.getPanelAddresses ===
      "object"
  );
}

export function validatePanelAddress(address: string): boolean {
  try {
    const url = new URL(address);
    return (
      (url.protocol === "http:" || url.protocol === "https:") && !!url.hostname
    );
  } catch {
    return false;
  }
}
