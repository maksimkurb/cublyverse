/** Fallback list used when `Intl.supportedValuesOf` is not available. */
const FALLBACK_TIMEZONES = [
  "UTC",
  "Europe/Lisbon",
  "Europe/London",
  "Europe/Berlin",
  "Europe/Warsaw",
  "Europe/Moscow",
  "Europe/Istanbul",
  "Asia/Dubai",
  "Asia/Yekaterinburg",
  "Asia/Oral",
  "Asia/Almaty",
  "Asia/Tokyo",
  "America/New_York",
  "America/Chicago",
  "America/Denver",
  "America/Los_Angeles",
  "Australia/Sydney",
];

/** Reads the IANA list from the runtime when it is supported. */
export function allTimezones(): string[] {
  const intl = Intl as unknown as { supportedValuesOf?: (key: string) => string[] };
  try {
    const zones = intl.supportedValuesOf?.("timeZone");
    if (zones && zones.length > 0) {
      return zones;
    }
  } catch {
    // Older engines throw instead of returning undefined.
  }
  return FALLBACK_TIMEZONES;
}

/** Options of the time zone selector, always including the current value. */
export function timezoneOptions(current: string): string[] {
  const zones = allTimezones();
  return zones.includes(current) ? zones : [current, ...zones];
}
