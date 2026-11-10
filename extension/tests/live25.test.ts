import assert from "node:assert/strict";
import test from "node:test";

import { classify25LiveResponse, test25LiveAccess } from "../src/live25.js";

function jsonResponse(status: number, body: unknown) {
  return {
    ok: status >= 200 && status < 300,
    status,
    json: async () => body,
  };
}

test("classifies a valid 25Live building-list response as successful", async () => {
  const result = await classify25LiveResponse(
    jsonResponse(200, { content: { data: { items: [] } } }),
  );
  assert.equal(result.kind, "success");
});

test("classifies unauthenticated 25Live responses", async () => {
  const result = await classify25LiveResponse(jsonResponse(401, {}));
  assert.equal(result.kind, "not-signed-in");
});

test("classifies malformed 25Live JSON shapes", async () => {
  const result = await classify25LiveResponse(jsonResponse(200, { building: "unexpected" }));
  assert.equal(result.kind, "malformed-response");
});

test("classifies unreadable 25Live JSON as malformed", async () => {
  const result = await classify25LiveResponse({
    ok: true,
    status: 200,
    json: async () => {
      throw new SyntaxError("invalid JSON");
    },
  });
  assert.equal(result.kind, "malformed-response");
});

test("reports network failures without retaining response data", async () => {
  const result = await test25LiveAccess(async () => {
    throw new TypeError("network unavailable");
  });
  assert.equal(result.kind, "network-error");
});

test("uses the fixed 25Live endpoint with the active browser session", async () => {
  let requestedUrl = "";
  let requestCredentials: RequestCredentials | undefined;
  const result = await test25LiveAccess(async (input, init) => {
    requestedUrl = String(input);
    requestCredentials = init?.credentials;
    return jsonResponse(200, { content: { data: { items: [] } } });
  });

  assert.equal(result.kind, "success");
  assert.equal(requestedUrl, "https://25live.collegenet.com/25live/data/unc/run/micro/building/list.json?caller=pro-BuildingService.getBuildings");
  assert.equal(requestCredentials, "include");
});
