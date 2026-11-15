import type { ApiError } from "./client";

/** Turns an API error object, an `Error` or anything else into a message. */
export function describeApiError(cause: unknown): string {
  if (cause instanceof Error) {
    return cause.message;
  }
  if (cause && typeof cause === "object" && "message" in cause) {
    const message = (cause as ApiError).message;
    if (typeof message === "string" && message.trim() !== "") {
      return message;
    }
  }
  return "Не удалось выполнить запрос";
}
