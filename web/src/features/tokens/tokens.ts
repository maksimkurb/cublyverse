import type { APIToken, APITokenScope, CreateTokenRequest } from "../../api/client";

/** One selectable permission of a token, as the create dialog shows it. */
export interface ScopeOption {
  value: APITokenScope;
  label: string;
  description: string;
}

/**
 * The scope registry, mirroring the server (`internal/auth.Scopes`). The server
 * validates the values; the client only offers the ones it knows about.
 */
export const SCOPE_OPTIONS: ScopeOption[] = [
  {
    value: "profile:read",
    label: "Профиль",
    description: "Кто вы: id, имя и часовой пояс (whoami)",
  },
  {
    value: "reminders:read",
    label: "Чтение напоминаний",
    description: "Смотреть список напоминаний и отдельные напоминания",
  },
  {
    value: "reminders:write",
    label: "Изменение напоминаний",
    description: "Создавать, менять, подтверждать и удалять напоминания",
  },
  {
    value: "craft:read",
    label: "Чтение «Бусинок»",
    description: "Смотреть каталог, запасы и закупки workspace (адрес /mcp/w/…)",
  },
  {
    value: "craft:write",
    label: "Изменение «Бусинок»",
    description: "Менять запасы, каталог и закупки workspace",
  },
];

/** Lifetimes the create dialog offers; `""` means no expiry. */
export const EXPIRY_OPTIONS = [
  { value: "", label: "Бессрочно" },
  { value: "30", label: "30 дней" },
  { value: "90", label: "90 дней" },
  { value: "365", label: "365 дней" },
] as const;

/** State of a token as the list shows it. */
export type TokenState = "active" | "revoked" | "expired";

/** Computes the state, revoking taking precedence over expiry. */
export function tokenState(token: APIToken, now: Date = new Date()): TokenState {
  if (token.revoked_at) {
    return "revoked";
  }
  if (token.expires_at && new Date(token.expires_at).getTime() <= now.getTime()) {
    return "expired";
  }
  return "active";
}

/** Russian label of a token state. */
export function tokenStateLabel(state: TokenState): string {
  switch (state) {
    case "revoked":
      return "Отозван";
    case "expired":
      return "Истёк";
    default:
      return "Активен";
  }
}

/** Human readable scope list for the token row. */
export function scopesLabel(scopes: APITokenScope[]): string {
  if (scopes.length === 0) {
    return "Без областей доступа";
  }
  return scopes.map((scope) => SCOPE_OPTIONS.find((option) => option.value === scope)?.label ?? scope).join(", ");
}

/** The visible head of a token, as it is shown in the list. */
export function prefixLabel(prefix: string): string {
  return `cv_pat_${prefix}…`;
}

/** Formats an ISO timestamp as `дд.мм.гггг`; an empty value means "never". */
export function formatTokenDate(value: string | null): string {
  if (!value) {
    return "—";
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return "—";
  }
  return new Intl.DateTimeFormat("ru-RU", {
    day: "2-digit",
    month: "2-digit",
    year: "numeric",
  }).format(date);
}

/** "Бессрочно" or "до дд.мм.гггг". */
export function expiryLabel(token: APIToken): string {
  return token.expires_at ? `до ${formatTokenDate(token.expires_at)}` : "Бессрочно";
}

/** Input of the create dialog before validation. */
export interface TokenFormValues {
  name: string;
  scopes: APITokenScope[];
  /** Raw select value: "" or a number of days as text. */
  expiry: string;
}

/** Empty form: the common case is full access without an expiry. */
export function emptyTokenForm(): TokenFormValues {
  return { name: "", scopes: ["profile:read", "reminders:read", "reminders:write"], expiry: "" };
}

/** Result of validating the create form. */
export type TokenFormResult =
  | { ok: true; body: CreateTokenRequest }
  | { ok: false; message: string };

/**
 * Validates the form and builds the request body. The rules repeat the server so
 * the user sees a Russian message before a round trip; the server still checks
 * everything.
 */
export function buildCreateTokenRequest(values: TokenFormValues): TokenFormResult {
  const name = values.name.trim();
  if (name.length < 1 || name.length > 80) {
    return { ok: false, message: "Название должно быть от 1 до 80 символов" };
  }
  if (values.scopes.length === 0) {
    return { ok: false, message: "Выберите хотя бы одну область доступа" };
  }

  let expiresInDays: number | null = null;
  if (values.expiry !== "") {
    const days = Number(values.expiry);
    if (!Number.isInteger(days) || days < 1 || days > 365) {
      return { ok: false, message: "Срок действия должен быть от 1 до 365 дней" };
    }
    expiresInDays = days;
  }

  return {
    ok: true,
    body: { name, scopes: values.scopes, expires_in_days: expiresInDays },
  };
}

/**
 * Copies a token to the clipboard. Returns false when the browser refuses: the
 * reveal screen then keeps the value visible so the user can copy it by hand.
 */
export async function copyToken(token: string): Promise<boolean> {
  try {
    if (!navigator.clipboard?.writeText) {
      return false;
    }
    await navigator.clipboard.writeText(token);
    return true;
  } catch {
    return false;
  }
}
