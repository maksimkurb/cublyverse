import createClient from "openapi-fetch";

import type { FetchLike, TelegramAuthResponse } from "../api/client";
import type { paths } from "../api/schema";

/** Telegram sign-in failed; the message is safe to show to the user. */
export class TelegramSignInError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "TelegramSignInError";
  }
}

/** Options of a single Telegram sign-in call. */
export interface TelegramSignInOptions {
  /** Raw `Telegram.WebApp.initData`. */
  initData: string;
  /** Fetch implementation, injectable in tests. */
  fetchImpl?: FetchLike;
  /** API prefix; defaults to the same-origin `/api/v1`. */
  baseUrl?: string;
}

/**
 * Exchanges Telegram `initData` for a session.
 *
 * The call is unauthenticated, so it uses its own client instead of the shared
 * one and cannot trigger the 401 refresh loop.
 */
export async function signInWithTelegram(options: TelegramSignInOptions): Promise<TelegramAuthResponse> {
  const initData = options.initData.trim();
  if (initData === "") {
    throw new TelegramSignInError("Нет данных Telegram: откройте приложение из чата с ботом.");
  }

  const client = createClient<paths>({
    baseUrl: options.baseUrl ?? "/api/v1",
    fetch: options.fetchImpl,
  });
  const { data, error, response } = await client.POST("/auth/telegram", {
    body: { init_data: initData },
  });

  if (data) {
    return data;
  }
  throw new TelegramSignInError(error?.message ?? `Сервер ответил ошибкой ${response.status}`);
}

/** Builds a sign-in function bound to a source of `initData`. */
export function createTelegramSignIn(getInitData: () => string, deps: Omit<TelegramSignInOptions, "initData"> = {}) {
  return () => signInWithTelegram({ ...deps, initData: getInitData() });
}
