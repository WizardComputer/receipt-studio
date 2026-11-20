// Pixel buffer for individual logos/QR images in Goja/Node. No Canvas required.
if (typeof globalThis.ImageData === 'undefined') {
  globalThis.ImageData = class ImageData {
    constructor(width, height) {
      if (!Number.isInteger(width) || !Number.isInteger(height) || width < 1 || height < 1 || width * height > 576 * 792) throw new Error('Slika je prevelika.');
      this.width = width; this.height = height;
      this.data = new Uint8ClampedArray(width * height * 4);
    }
  };
}
// RPE clones plain printer/style definitions. Preserve undefined fields and
// arrays in Goja too; JSON round-tripping would silently drop those fields.
if (typeof globalThis.structuredClone === 'undefined') {
  globalThis.structuredClone = value => {
    const seen = new Map();
    const copy = item => {
      if (typeof item === 'function') throw new Error('Cannot clone functions');
      if (item === null || typeof item !== 'object') return item;
      if (seen.has(item)) return seen.get(item);
      if (ArrayBuffer.isView(item)) return new item.constructor(item);
      const result = Array.isArray(item) ? [] : {};
      seen.set(item, result);
      for (const key of Object.keys(item)) result[key] = copy(item[key]);
      return result;
    };
    return copy(value);
  };
}
