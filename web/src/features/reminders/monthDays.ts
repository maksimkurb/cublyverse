/** Result of reading the «Дни месяца» text input. */
export interface MonthDaysParse {
  /** Valid days, sorted and deduplicated. */
  days: number[];
  /** Tokens that are not day numbers 1..31, in input order. */
  invalid: string[];
}

/**
 * Parses days of month typed by the user.
 *
 * Separators are commas, spaces and semicolons (any mix), so `1, 15, 30`,
 * `1 15 30` and `1;15;30` are the same input.
 */
export function parseMonthDays(input: string): MonthDaysParse {
  const tokens = input
    .split(/[,;\s]+/)
    .map((token) => token.trim())
    .filter((token) => token !== "");

  const days = new Set<number>();
  const invalid: string[] = [];
  for (const token of tokens) {
    if (!/^\d+$/.test(token)) {
      invalid.push(token);
      continue;
    }
    const day = Number(token);
    if (!Number.isInteger(day) || day < 1 || day > 31) {
      invalid.push(token);
      continue;
    }
    days.add(day);
  }

  return { days: [...days].sort((a, b) => a - b), invalid };
}

/** Formats stored days back into the text input. */
export function formatMonthDays(days: number[]): string {
  return [...new Set(days)].sort((a, b) => a - b).join(", ");
}
