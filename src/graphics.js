import './vendor/qrcode-generator-2.0.4.js';

// Only individual graphics use pixels. The receipt and its text never do.
export function qrMatrix(value, level = 'm', native = false) {
  const qr = globalThis.ReceiptQRCode(0, level.toUpperCase());
  globalThis.ReceiptQRCode.stringToBytes = native
    ? globalThis.ReceiptQRCode.stringToBytesFuncs.default
    : globalThis.ReceiptQRCode.stringToBytesFuncs['UTF-8'];
  const mode = native && /^\d+$/.test(value) ? 'Numeric'
    : native && /^[0-9A-Z $%*+./:-]+$/.test(value) ? 'Alphanumeric' : 'Byte';
  qr.addData(value, mode);
  try { qr.make(); } catch { throw new Error('QR sadržaj je predug za odabranu korekciju greške.'); }
  const size = qr.getModuleCount();
  return { size, pixels: Array.from({ length: size * size }, (_, i) => qr.isDark(Math.floor(i / size), i % size) ? 1 : 0) };
}

export function packedPixels(width, height, pixels) {
  const stride = Math.ceil(width / 8), bytes = new Uint8Array(stride * height);
  for (let y = 0; y < height; y++) for (let x = 0; x < width; x++) {
    if (pixels[y * width + x]) bytes[y * stride + (x >> 3)] |= 128 >> (x & 7);
  }
  return Array.from(bytes);
}

export function graphicImage(element, grid, paperWidth, value) {
  let width, height, pixels;
  if (element.type === 'qr') {
    const matrix = qrMatrix(value, element.errorLevel);
    width = height = (matrix.size + 8) * element.moduleSize;
    if (width > grid.width) throw new Error('QR kod s bijelim rubom je preširok. Smanji modul ili proširi element.');
    pixels = new Uint8Array(width * height);
    for (let y = 0; y < height; y++) for (let x = 0; x < width; x++) {
      const mx = Math.floor(x / element.moduleSize) - 4, my = Math.floor(y / element.moduleSize) - 4;
      if (mx >= 0 && my >= 0 && mx < matrix.size && my < matrix.size) pixels[y * width + x] = matrix.pixels[my * matrix.size + mx];
    }
  } else {
    if (!element.image) throw new Error('Odaberi PNG, JPEG ili WebP sliku za logo.');
    const source = element.image;
    width = Math.min(element.imageWidth, grid.width);
    height = Math.max(1, Math.round(width * source.height / source.width));
    if (height > 768) throw new Error('Slika je previsoka. Smanji širinu slike ili učitaj širi logo.');
    pixels = new Uint8Array(width * height);
    for (let y = 0; y < height; y++) for (let x = 0; x < width; x++) {
      const sx = Math.min(source.width - 1, Math.floor(x * source.width / width));
      const sy = Math.min(source.height - 1, Math.floor(y * source.height / height));
      pixels[y * width + x] = parseInt(source.gray.slice((sy * source.width + sx) * 2, (sy * source.width + sx) * 2 + 2), 16) < element.threshold ? 1 : 0;
    }
  }
  // White padding positions just this graphic without changing text margins.
  const left = grid.x + (element.align === 'right' ? grid.width - width : element.align === 'center' ? Math.floor((grid.width - width) / 2) : 0);
  const paddedHeight = Math.ceil(height / 8) * 8;
  const stride = paperWidth / 8, packed = new Uint8Array(stride * paddedHeight);
  for (let y = 0; y < height; y++) for (let x = 0; x < width; x++) {
    if (pixels[y * width + x]) {
      packed[y * stride + ((left + x) >> 3)] |= 128 >> ((left + x) & 7);
    }
  }
  return { width: paperWidth, height: paddedHeight, data: packed };
}

// Already monochrome: send packed bits through RPE's raw adapter. Running the
// RGBA flatten/dither pipeline again is redundant and slow in Goja. These are
// the same raster / 24-dot column commands emitted by RPE.image().
export function printGraphic(encoder, image, star) {
  const {width, height, data} = image, stride = width / 8;
  if (!star) {
    for (let start = 0; start < height; start += 255) {
      const rows = Math.min(255, height - start);
      encoder.raw([29, 118, 48, 0, stride & 255, stride >> 8, rows, 0, ...data.slice(start * stride, (start + rows) * stride)]);
    }
  } else {
    encoder.raw([27, 48]);
    for (let start = 0; start < height; start += 24) {
      const column = new Uint8Array(width * 3);
      for (let y = 0; y < 24 && start + y < height; y++) for (let bx = 0; bx < stride; bx++) {
        const value = data[(start + y) * stride + bx];
        if (!value) continue;
        for (let bit = 0; bit < 8; bit++) if (value & (128 >> bit)) column[(bx * 8 + bit) * 3 + (y >> 3)] |= 128 >> (y & 7);
      }
      encoder.raw([27, 88, width & 255, width >> 8, ...column, 10, 13]);
    }
    encoder.raw([27, 122, 1]);
  }
  encoder.newline();
}

export function graphicPath(graphic) {
  const { width, height, data } = graphic, stride = Math.ceil(width / 8);
  let path = '';
  for (let y = 0; y < height; y++) {
    let start = -1;
    for (let x = 0; x <= width; x++) {
      const dark = x < width && (data[y * stride + (x >> 3)] & (128 >> (x & 7)));
      if (dark && start < 0) start = x;
      if (!dark && start >= 0) { path += `M${start} ${y}h${x - start}v1H${start}z`; start = -1; }
    }
  }
  return path;
}

// Bounds of printed dots, excluding white alignment padding and quiet zone.
export function graphicBounds(graphics) {
  let left = Infinity, top = Infinity, right = -1, bottom = -1;
  for (const g of graphics) {
    const stride = Math.ceil(g.width / 8);
    for (let y = 0; y < g.height; y++) for (let bx = 0; bx < stride; bx++) {
      const value = g.data[y * stride + bx];
      if (!value) continue;
      for (let bit = 0; bit < 8; bit++) {
        const x = bx * 8 + bit;
        if (x < g.width && value & (128 >> bit)) {
          left = Math.min(left, g.x + x); right = Math.max(right, g.x + x);
          top = Math.min(top, g.y + y); bottom = Math.max(bottom, g.y + y);
        }
      }
    }
  }
  return right < 0 ? null : { x: left, y: top, width: right - left + 1, height: bottom - top + 1 };
}
