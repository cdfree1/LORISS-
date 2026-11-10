import { LIVE25_BUILDINGS_URL } from "./endpoints.js";

export type Live25AccessResult =
  | { kind: "success"; message: string }
  | { kind: "not-signed-in"; message: string }
  | { kind: "malformed-response"; message: string }
  | { kind: "network-error"; message: string }
  | { kind: "api-error"; message: string };

interface ResponseLike {
  readonly ok: boolean;
  readonly status: number;
  json(): Promise<unknown>;
}

interface FetchLike {
  (input: string | URL, init?: RequestInit): Promise<ResponseLike>;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}

function hasBuildingListShape(value: unknown): boolean {
  return (
    isRecord(value) &&
    isRecord(value.content) &&
    isRecord(value.content.data) &&
    Array.isArray(value.content.data.items)
  );
}

export async function classify25LiveResponse(response: ResponseLike): Promise<Live25AccessResult> {
  if (!response.ok) {
    if (response.status === 401 || response.status === 403) {
      return {
        kind: "not-signed-in",
        message: `25Live did not authorize this request (${response.status}). Sign in, then try again.`,
      };
    }

    return {
      kind: "api-error",
      message: `25Live returned HTTP ${response.status}. No data was retained.`,
    };
  }

  try {
    const body: unknown = await response.json();
    if (!hasBuildingListShape(body)) {
      return {
        kind: "malformed-response",
        message: "25Live returned JSON, but not the expected building-list response.",
      };
    }

    return {
      kind: "success",
      message: "25Live access confirmed with a read-only building-list request.",
    };
  } catch {
    return {
      kind: "malformed-response",
      message: "25Live returned a response that could not be read as the expected JSON.",
    };
  }
}

export async function test25LiveAccess(fetchFn: FetchLike = fetch): Promise<Live25AccessResult> {
  try {
    const response = await fetchFn(LIVE25_BUILDINGS_URL, {
      method: "GET",
      credentials: "include",
      headers: { Accept: "application/json" },
    });
    return classify25LiveResponse(response);
  } catch {
    return {
      kind: "network-error",
      message: "Could not reach 25Live. Check your connection and try again.",
    };
  }
}
