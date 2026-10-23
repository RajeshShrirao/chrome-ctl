/** Error carrying a JSON-RPC error code through the action layer. */
export class RpcFault extends Error {
  readonly code: number;

  constructor(code: number, message: string) {
    super(message);
    this.name = "RpcFault";
    this.code = code;
  }
}

export function errorText(error: unknown): string {
  if (error instanceof Error && error.message !== "") {
    return error.message;
  }
  if (typeof error === "string" && error !== "") {
    return error;
  }
  return "unknown error";
}
