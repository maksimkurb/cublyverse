/** A colour typed into a search field, for example `#a1b2c3`; null otherwise. */
export function hexFromQuery(query: string): string | null {
  // The "#" is required: six digits alone may be an article number.
  const match = /^#[0-9a-f]{6}$/i.exec(query.trim());
  return match ? match[0].toUpperCase() : null;
}

/**
 * The colour of a patch of RGBA pixels: the median of each channel, so a glint
 * or a speck of shadow under the finger does not change the result.
 */
export function medianColor(rgba: Uint8ClampedArray): string {
  const pixels = Math.floor(rgba.length / 4);
  if (pixels === 0) {
    return "";
  }
  const channel = (offset: number) => {
    const values = new Uint8Array(pixels);
    for (let index = 0; index < pixels; index++) {
      values[index] = rgba[index * 4 + offset]!;
    }
    values.sort();
    return values[Math.floor(pixels / 2)]!.toString(16).padStart(2, "0");
  };
  return `#${channel(0)}${channel(1)}${channel(2)}`.toUpperCase();
}
