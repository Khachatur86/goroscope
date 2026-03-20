/**
 * Minimal animated GIF89a encoder.
 *
 * Algorithm
 *  1. Collect all unique RGB colors across every frame (sampling every other pixel).
 *  2. Reduce to ≤256 representative colors via median-cut quantization.
 *  3. Build a direct Map<packedRGB, paletteIdx> for O(1) per-pixel quantization.
 *  4. LZW-compress each frame's palette-index stream.
 *  5. Write the GIF89a byte sequence with a Netscape looping extension.
 */

// ── Types ─────────────────────────────────────────────────────────────────────

type RGB = [number, number, number];

export interface GifFrame {
  /** Raw RGBA pixel data (Uint8ClampedArray from ImageData), row-major. */
  pixels: Uint8ClampedArray;
  /** Frame delay in centiseconds (1/100 s). */
  delayCs: number;
}

// ── ByteWriter ────────────────────────────────────────────────────────────────

/** Growable byte buffer — avoids JS spread-argument limit on large arrays. */
class ByteWriter {
  private buf: Uint8Array;
  private pos = 0;

  constructor(initial = 65_536) {
    this.buf = new Uint8Array(initial);
  }

  private ensure(n: number) {
    if (this.pos + n > this.buf.length) {
      const next = new Uint8Array(Math.max(this.buf.length * 2, this.pos + n));
      next.set(this.buf);
      this.buf = next;
    }
  }

  byte(b: number) {
    this.ensure(1);
    this.buf[this.pos++] = b & 0xff;
  }

  le16(n: number) {
    this.ensure(2);
    this.buf[this.pos++] = n & 0xff;
    this.buf[this.pos++] = (n >> 8) & 0xff;
  }

  bytes(arr: ArrayLike<number>) {
    this.ensure(arr.length);
    for (let i = 0; i < arr.length; i++) {
      this.buf[this.pos++] = (arr[i] as number) & 0xff;
    }
  }

  ascii(s: string) {
    this.ensure(s.length);
    for (let i = 0; i < s.length; i++) {
      this.buf[this.pos++] = s.charCodeAt(i) & 0xff;
    }
  }

  finish(): Uint8Array {
    return this.buf.slice(0, this.pos);
  }
}

// ── Median-cut palette builder ─────────────────────────────────────────────────

function medianCut(colors: RGB[], maxColors: number): RGB[] {
  if (colors.length === 0) return [[0, 0, 0]];
  if (colors.length <= maxColors) return colors.slice();

  let boxes: RGB[][] = [colors.slice()];

  while (boxes.length < maxColors) {
    // Pick the box with the widest channel range.
    let bestBox = -1, bestRange = -1, bestAxis: 0 | 1 | 2 = 0;

    for (let i = 0; i < boxes.length; i++) {
      const box = boxes[i];
      for (const axis of [0, 1, 2] as const) {
        let mn = 255, mx = 0;
        for (const c of box) {
          if (c[axis] < mn) mn = c[axis];
          if (c[axis] > mx) mx = c[axis];
        }
        if (mx - mn > bestRange) {
          bestRange = mx - mn;
          bestBox = i;
          bestAxis = axis;
        }
      }
    }

    if (bestRange === 0) break;

    const box = boxes[bestBox];
    box.sort((a, b) => a[bestAxis] - b[bestAxis]);
    const mid = Math.ceil(box.length / 2);
    boxes.splice(bestBox, 1, box.slice(0, mid), box.slice(mid));
  }

  return boxes.map((box) => {
    let r = 0, g = 0, b = 0;
    for (const c of box) { r += c[0]; g += c[1]; b += c[2]; }
    const n = box.length;
    return [Math.round(r / n), Math.round(g / n), Math.round(b / n)] as RGB;
  });
}

// ── Color quantization ────────────────────────────────────────────────────────

/**
 * Collect unique RGB colors (sampling every 2nd pixel).
 * Returns at most ~50k entries for a typical timeline frame set.
 */
function collectColors(frames: GifFrame[], width: number, height: number): RGB[] {
  const seen = new Map<number, RGB>();
  for (const f of frames) {
    for (let y = 0; y < height; y += 2) {
      for (let x = 0; x < width; x += 2) {
        const i = (y * width + x) << 2;
        const r = f.pixels[i], g = f.pixels[i + 1], b = f.pixels[i + 2];
        const key = (r << 16) | (g << 8) | b;
        if (!seen.has(key)) seen.set(key, [r, g, b]);
      }
    }
  }
  return [...seen.values()];
}

/**
 * Build a palette of exactly 256 colors for the given frames,
 * and a Map from packed-RGB to palette index.
 */
function buildPaletteAndMap(
  frames: GifFrame[],
  width: number,
  height: number,
): { palette: RGB[]; colorMap: Map<number, number> } {
  const unique = collectColors(frames, width, height);
  const reduced = medianCut(unique, 256);

  // Pad to exactly 256 entries.
  while (reduced.length < 256) reduced.push([0, 0, 0]);
  const palette = reduced.slice(0, 256) as RGB[];

  // Build map: unique sampled colors → nearest palette index.
  const colorMap = new Map<number, number>();
  for (const [r, g, b] of unique) {
    const key = (r << 16) | (g << 8) | b;
    let bestIdx = 0, bestDist = Infinity;
    for (let p = 0; p < 256; p++) {
      const dr = r - palette[p][0];
      const dg = g - palette[p][1];
      const db = b - palette[p][2];
      const dist = dr * dr + dg * dg + db * db;
      if (dist < bestDist) { bestDist = dist; bestIdx = p; }
    }
    colorMap.set(key, bestIdx);
  }

  return { palette, colorMap };
}

/** Map a single frame's RGBA pixels to a Uint8Array of palette indices. */
function quantizeFrame(
  pixels: Uint8ClampedArray,
  palette: RGB[],
  colorMap: Map<number, number>,
): Uint8Array {
  const n = pixels.length >> 2;
  const out = new Uint8Array(n);

  for (let i = 0; i < n; i++) {
    const r = pixels[i * 4], g = pixels[i * 4 + 1], b = pixels[i * 4 + 2];
    const key = (r << 16) | (g << 8) | b;
    let idx = colorMap.get(key);

    if (idx === undefined) {
      // Color not sampled earlier — nearest-neighbor fallback.
      let bestIdx = 0, bestDist = Infinity;
      for (let p = 0; p < 256; p++) {
        const dr = r - palette[p][0];
        const dg = g - palette[p][1];
        const db = b - palette[p][2];
        const dist = dr * dr + dg * dg + db * db;
        if (dist < bestDist) { bestDist = dist; bestIdx = p; }
      }
      idx = bestIdx;
      colorMap.set(key, idx); // cache for future pixels
    }

    out[i] = idx;
  }

  return out;
}

// ── LZW compression ───────────────────────────────────────────────────────────

/**
 * GIF LZW encoder.
 * Returns the raw byte array (already packed into sub-blocks of ≤255 bytes,
 * with a terminal 0x00 block).
 */
function lzwCompress(indices: Uint8Array, minCodeSize: number): Uint8Array {
  const clearCode = 1 << minCodeSize;
  const endCode   = clearCode + 1;

  // Direct-addressed code table: entry = table[prefix * 256 + suffix].
  // Prefix in 0..4095, suffix 0..255 → 4096 * 256 = 1 048 576 entries.
  const table = new Int32Array(4096 * 256).fill(-1);

  let codeSize = minCodeSize + 1;
  let nextCode = endCode + 1;

  const resetTable = () => {
    table.fill(-1);
    codeSize = minCodeSize + 1;
    nextCode = endCode + 1;
  };

  // Bit-packed output.
  const raw: number[] = [];
  let bitBuf = 0, bitCount = 0;

  const emit = (code: number) => {
    bitBuf |= code << bitCount;
    bitCount += codeSize;
    while (bitCount >= 8) {
      raw.push(bitBuf & 0xff);
      bitBuf >>>= 8;
      bitCount -= 8;
    }
  };

  resetTable();
  emit(clearCode);

  let prefix = indices[0];

  for (let i = 1; i < indices.length; i++) {
    const suffix = indices[i];
    const key    = prefix * 256 + suffix;
    const found  = table[key];

    if (found >= 0) {
      prefix = found;
    } else {
      emit(prefix);

      if (nextCode < 4096) {
        table[key] = nextCode++;
        if (nextCode > (1 << codeSize) && codeSize < 12) codeSize++;
      }

      // Table full → emit clear, reset.
      if (nextCode >= 4096) {
        emit(clearCode);
        resetTable();
      }

      prefix = suffix;
    }
  }

  emit(prefix);
  emit(endCode);
  if (bitCount > 0) raw.push(bitBuf & 0xff);

  // Pack into GIF sub-blocks (max 255 bytes each) + trailing 0x00 terminator.
  const out = new ByteWriter(raw.length + Math.ceil(raw.length / 255) + 1);
  for (let i = 0; i < raw.length; i += 255) {
    const end = Math.min(i + 255, raw.length);
    out.byte(end - i);
    for (let j = i; j < end; j++) out.byte(raw[j]);
  }
  out.byte(0); // block terminator

  return out.finish();
}

// ── GIF89a writer ─────────────────────────────────────────────────────────────

/**
 * Encode a sequence of frames as an animated GIF89a.
 *
 * @param frames  Array of { pixels: RGBA Uint8ClampedArray, delayCs: number }
 * @param width   Canvas width in logical (1×) pixels.
 * @param height  Canvas height in logical (1×) pixels.
 * @returns       Uint8Array containing the complete GIF file.
 */
export function encodeAnimatedGIF(
  frames: GifFrame[],
  width: number,
  height: number,
): Uint8Array {
  if (frames.length === 0) throw new Error("encodeAnimatedGIF: no frames");

  const { palette, colorMap } = buildPaletteAndMap(frames, width, height);

  const w = new ByteWriter(width * height * frames.length);

  // ── GIF89a header ──────────────────────────────────────────────────────────
  w.ascii("GIF89a");

  // Logical Screen Descriptor
  w.le16(width);
  w.le16(height);
  w.byte(0xf7); // GCT present; color resolution = 8; GCT size = 256
  w.byte(0x00); // background color index
  w.byte(0x00); // pixel aspect ratio

  // Global Color Table (256 × 3 bytes)
  for (const [r, g, b] of palette) { w.byte(r); w.byte(g); w.byte(b); }

  // Netscape Application Extension — loop forever
  w.byte(0x21); w.byte(0xff); w.byte(11);
  w.ascii("NETSCAPE"); w.ascii("2.0");
  w.byte(3); w.byte(1);
  w.le16(0); // loop count 0 = infinite
  w.byte(0); // block terminator

  const MIN_CODE = 8; // for 256-color palette

  for (const frame of frames) {
    const indices    = quantizeFrame(frame.pixels, palette, colorMap);
    const compressed = lzwCompress(indices, MIN_CODE);

    // Graphic Control Extension
    w.byte(0x21); w.byte(0xf9); w.byte(4);
    w.byte(0x08); // disposal = restore to bg color; no user input; no transparency
    w.le16(frame.delayCs);
    w.byte(0x00); // transparent color index (unused)
    w.byte(0x00); // block terminator

    // Image Descriptor
    w.byte(0x2c);
    w.le16(0); w.le16(0);     // left, top
    w.le16(width); w.le16(height);
    w.byte(0x00); // no local color table; not interlaced

    // LZW-compressed image data
    w.byte(MIN_CODE);
    w.bytes(compressed);
  }

  // Trailer
  w.byte(0x3b);

  return w.finish();
}
