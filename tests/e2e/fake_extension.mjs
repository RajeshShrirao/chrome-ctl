#!/usr/bin/env node
import process from "node:process";

function arg(name, fallback) {
  const index = process.argv.indexOf(name);
  return index >= 0 ? process.argv[index + 1] : fallback;
}

const addr = arg("--addr", "127.0.0.1:9229");
const token = arg("--token", "");
const tabCount = Number(arg("--tabs", "3"));

function rejectedUnauthenticated() {
  return new Promise((resolve, reject) => {
    const ws = new WebSocket(`ws://${addr}/ws`);
    const timer = setTimeout(() => reject(new Error("unauthenticated handshake did not fail")), 3000);
    ws.onopen = () => {
      clearTimeout(timer);
      ws.close();
      reject(new Error("unauthenticated handshake was accepted"));
    };
    ws.onerror = () => {
      clearTimeout(timer);
      resolve();
    };
    ws.onclose = () => {
      clearTimeout(timer);
      resolve();
    };
  });
}

function connect() {
  return new Promise((resolve, reject) => {
    const ws = new WebSocket(`ws://${addr}/ws?token=${encodeURIComponent(token)}`);
    const timer = setTimeout(() => reject(new Error("authenticated handshake timed out")), 3000);
    ws.onopen = () => {
      clearTimeout(timer);
      resolve(ws);
    };
    ws.onerror = () => {
      clearTimeout(timer);
      reject(new Error("authenticated handshake failed"));
    };
  });
}

await rejectedUnauthenticated();

const ws = await connect();
console.log("CONNECTED");

let heartbeat;
let ready = false;

function send(payload) {
  ws.send(JSON.stringify(payload));
}

ws.onmessage = (event) => {
  let message;
  try {
    message = JSON.parse(String(event.data));
  } catch {
    console.error("invalid JSON from daemon");
    return;
  }

  if (message.method === "system.ready") {
    ready = true;
    console.log("READY", JSON.stringify(message.params));
    return;
  }

  if (message.method === "session.status" && message.id != null) {
    send({
      jsonrpc: "2.0",
      id: message.id,
      result: {
        activeTab: { tabId: 1, title: "Fake Tab", url: "https://example.test" },
        tabCount,
        debuggerAttached: false,
      },
    });
  }
};

ws.onclose = () => {
  clearInterval(heartbeat);
  console.log("CLOSED");
  if (!ready) process.exit(1);
  process.exit(0);
};

ws.onerror = () => {
  console.error("websocket error");
};

heartbeat = setInterval(() => {
  if (ws.readyState === WebSocket.OPEN) {
    send({ jsonrpc: "2.0", method: "system.ping", params: { timestamp: Math.floor(Date.now() / 1000) } });
  }
}, 2000);

for (const signal of ["SIGINT", "SIGTERM"]) {
  process.on(signal, () => {
    clearInterval(heartbeat);
    ws.close();
    process.exit(0);
  });
}
