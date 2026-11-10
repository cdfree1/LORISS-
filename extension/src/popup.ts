import {
  type Live25AccessResult,
  test25LiveAccess,
} from "./live25.js";

function requireElement<T extends HTMLElement>(id: string): T {
  const element = document.getElementById(id);
  if (!(element instanceof HTMLElement)) {
    throw new Error(`Missing required popup element: ${id}`);
  }
  return element as T;
}

const live25Button = requireElement<HTMLButtonElement>("test-25live");
const live25Status = requireElement<HTMLParagraphElement>("25live-status");

function setStatus(element: HTMLParagraphElement, result: Live25AccessResult): void {
  element.className = `status ${result.kind === "success" ? "success" : "error"}`;
  element.textContent = result.message;
}

async function runAction(
  button: HTMLButtonElement,
  status: HTMLParagraphElement,
  pendingMessage: string,
  action: () => Promise<Live25AccessResult>,
): Promise<void> {
  button.disabled = true;
  status.className = "status pending";
  status.textContent = pendingMessage;
  try {
    setStatus(status, await action());
  } finally {
    button.disabled = false;
  }
}

live25Button.addEventListener("click", () =>
  runAction(live25Button, live25Status, "Checking 25Live…", test25LiveAccess),
);
