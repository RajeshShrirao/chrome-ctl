import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

import {
  CAPABILITIES,
  DEFAULT_RPC_TIMEOUT_MS,
  ErrorCodes,
  Methods,
  PROTOCOL_VERSION,
  rpcTimeoutMs,
} from "../src/shared/protocol.ts";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const contractPath = path.resolve(__dirname, "../../internal/protocol/contract.json");

interface ContractMethod {
  name: string;
  go: string;
  ts: string;
  daemonLocal?: boolean;
}

interface ContractError {
  go: string;
  ts: string;
  code: number;
}

interface ContractTimeoutGroup {
  go: string;
  name: string;
  ms: number;
  methods: string[];
}

interface Contract {
  protocolVersion: string;
  capabilities: string[];
  methods: ContractMethod[];
  errors: ContractError[];
  timeouts: {
    defaultMs: number;
    groups: ContractTimeoutGroup[];
  };
}

test("contract matches TypeScript protocol constants", () => {
  const contract: Contract = JSON.parse(fs.readFileSync(contractPath, "utf-8"));

  assert.equal(PROTOCOL_VERSION, contract.protocolVersion);
  assert.deepEqual([...CAPABILITIES], contract.capabilities);

  // Every contract method must exist on Methods
  for (const method of contract.methods) {
    const key = method.ts as keyof typeof Methods;
    assert.ok(key in Methods, `missing method in TS Methods: ${method.ts}`);
    assert.equal(Methods[key], method.name);
  }

  // Every contract error code must exist on ErrorCodes
  for (const error of contract.errors) {
    const key = error.ts as keyof typeof ErrorCodes;
    assert.ok(key in ErrorCodes, `missing error in TS ErrorCodes: ${error.ts}`);
    assert.equal(ErrorCodes[key], error.code);
  }

  // Timeout mapping matches contract
  for (const group of contract.timeouts.groups) {
    for (const methodName of group.methods) {
      assert.equal(rpcTimeoutMs(methodName), group.ms, `timeout for ${methodName}`);
    }
  }
  assert.equal(rpcTimeoutMs("unknown.method"), DEFAULT_RPC_TIMEOUT_MS);
});

test("round-trip JSON-RPC message serialization", () => {
  const request = {
    jsonrpc: "2.0",
    id: "req-1",
    method: Methods.actionClick,
    params: {
      id: 42,
      doubleClick: false,
      seed: "abc12345",
    },
  };

  const serialized = JSON.stringify(request);
  const parsed = JSON.parse(serialized);
  assert.deepEqual(parsed, request);

  const response = {
    jsonrpc: "2.0",
    id: "req-1",
    result: {
      success: true,
      action: "click",
      seed: "abc12345",
      durationMs: 35,
    },
  };

  const resSerialized = JSON.stringify(response);
  const resParsed = JSON.parse(resSerialized);
  assert.deepEqual(resParsed, response);

  const errorResponse = {
    jsonrpc: "2.0",
    id: "req-2",
    error: {
      code: ErrorCodes.dispatchedUnconfirmed,
      message: "dispatch timed out",
      data: {
        method: Methods.actionClick,
        timeoutMs: 60000,
      },
    },
  };

  const errSerialized = JSON.stringify(errorResponse);
  const errParsed = JSON.parse(errSerialized);
  assert.deepEqual(errParsed, errorResponse);
});
