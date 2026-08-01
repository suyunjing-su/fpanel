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

export function getPanelAddresses(): PanelAddress[] {
  return readPanelAddresses();
}

export function getCurrentPanelAddress(): string | null {
  return readPanelAddresses().find((item) => item.inx)?.address || null;
}

export function savePanelAddress(name: string, address: string): void {
  const addresses = readPanelAddresses();
  const existing = addresses.find((item) => item.name === name);
  const next = addresses.filter((item) => item.name !== name);
  next.push({ name, address, inx: existing?.inx ?? next.length === 0 });
  writePanelAddresses(next);
}

export function setCurrentPanelAddress(name: string): void {
  writePanelAddresses(
    readPanelAddresses().map((item) => ({ ...item, inx: item.name === name })),
  );
}

export function deletePanelAddress(name: string): void {
  const remaining = readPanelAddresses().filter((item) => item.name !== name);
  if (remaining.length > 0 && !remaining.some((item) => item.inx)) {
    remaining[0].inx = true;
  }
  writePanelAddresses(remaining);
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
