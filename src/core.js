import { cp852 } from './cp852.js';
import { ReceiptPrinterEncoder } from './rpe-runtime.js';
import { EscPosPreview } from './escpos-preview.js';
import { graphicImage, printGraphic, qrMatrix } from './graphics.js';

export const printerLanguages = {
  'esc-pos': { label: 'ESC/POS', codePage: 18 },
  'star-prnt': { label: 'StarPRNT', codePage: 5 },
  'star-line': { label: 'Star Line', codePage: 5 },
};
export const profiles = {
  wide: { name: '80 mm · 576 dots', paper: 80, width: 576, dpi: 203, charWidth: 12, charHeight: 24, codePage: 18 },
  narrow: { name: '58 mm · 384 dots', paper: 58, width: 384, dpi: 203, charWidth: 12, charHeight: 24, codePage: 18 },
};
export const sampleData = {
  shop: 'MALA RADNJA', address: 'Ferhadija 12, Sarajevo', number: '2026 / 0042',
  date: '04.11.2026. 12:30', total: '18,50 KM',
  items: [
    { name: 'Domaća kafa', qty: '2', price: '5,00' },
    { name: 'Čaj od šipka i šumsko voće', qty: '1', price: '3,50' },
    { name: 'Sendvič s povrćem', qty: '2', price: '10,00' },
  ],
};
export function newElement(type, id = crypto.randomUUID()) {
  const base = { id, type, x: 0, width: 100, scale: 1, bold: false, underline: false, align: 'left', gap: 8 };
  if (type === 'text') return { ...base, text: 'Novi tekst' };
  if (type === 'qr') return { ...base, align: 'center', content: 'https://example.com', moduleSize: 4, errorLevel: 'm', qrMode: 'native' };
  if (type === 'image') return { ...base, align: 'center', image: null, imageWidth: 192, threshold: 160 };
  if (type === 'table') return { ...base, source: 'items', columns: [
    { label: 'Artikal', key: 'name', width: 60, align: 'left' },
    { label: 'Kol.', key: 'qty', width: 15, align: 'right' },
    { label: 'Iznos', key: 'price', width: 25, align: 'right' },
  ] };
  throw new Error('Nepoznat element.');
}
export function initialTemplate() {
  const text = (id, value, props = {}) => ({ ...newElement('text', id), text: value, ...props });
  return { version: 1, name: 'Moj prvi račun', profile: 'wide', codePage: 18, cut: true, elements: [
    text('top-space', '', { name: 'Gornji razmak', gap: 20 }),
    text('brand', '{{shop}}', { name: 'Naziv firme', align: 'center', scale: 2, bold: true, gap: 12 }),
    text('address', '{{address}}', { name: 'Adresa firme', align: 'center', gap: 28 }),
    text('number', 'Račun: {{number}}\n{{date}}', { name: 'Broj i datum računa', gap: 20 }),
    { ...newElement('table', 'items'), name: 'Stavke računa' },
    text('total', 'UKUPNO: {{total}}', { name: 'Ukupan iznos', align: 'right', bold: true, scale: 2, gap: 28 }),
    text('thanks', 'Hvala na posjeti!\nČuvajte svoj račun.', { name: 'Zahvala', align: 'center' }),
  ] };
}
const fail = message => { throw new Error(message); };
const number = (n, min, max) => typeof n === 'number' && Number.isFinite(n) && n >= min && n <= max;
export function validateTemplate(t) {
  if (!t || t.version !== 1 || !profiles[t.profile] || typeof t.name !== 'string' || t.name.length > 120 ||
      !Number.isInteger(t.codePage) || !number(t.codePage, 0, 255) || typeof t.cut !== 'boolean' ||
      !Array.isArray(t.elements) || t.elements.length > 100) fail('Neispravan predložak (verzija 1, do 100 elemenata).');
  if (t.outputTarget !== undefined && !['printer', 'virtual-printer'].includes(t.outputTarget)) fail('Nepoznata vrsta izvoza.');
  if (t.language !== undefined && !Object.hasOwn(printerLanguages, t.language)) fail('Nepoznat protokol printera.');
  if ((t.language || 'esc-pos') !== 'esc-pos' && t.outputTarget === 'virtual-printer') fail('Virtual Printer izvoz podržava samo ESC/POS.');
  const ids = new Set();
  for (const e of t.elements) {
    if (!e || !['text', 'table', 'qr', 'image'].includes(e.type) || typeof e.id !== 'string' || ids.has(e.id)) fail('Neispravan element ili dupli ID.');
    ids.add(e.id);
    if (!number(e.x, 0, 90) || !number(e.width, 10, 100) || e.x + e.width > 100.01 ||
        ![1, 2].includes(e.scale) || !number(e.gap, 0, 120) || !['left', 'center', 'right'].includes(e.align) ||
        typeof e.bold !== 'boolean' || typeof e.underline !== 'boolean') fail('Neispravna svojstva elementa.');
    if (e.type === 'text' && (typeof e.text !== 'string' || e.text.length > 10000)) fail('Tekst mora biti kraći od 10.000 znakova.');
    if (e.type === 'qr') {
      if (typeof e.content !== 'string' || e.content.length > 10000 || !Number.isInteger(e.moduleSize) || !number(e.moduleSize, 1, 8) ||
          !['l', 'm', 'q', 'h'].includes(e.errorLevel) || !['native', 'image'].includes(e.qrMode)) fail('Neispravna svojstva QR koda.');
      if (e.qrMode === 'native' && (e.x !== 0 || e.width !== 100)) fail('Nativni QR koristi punu širinu papira. Za slobodan X položaj odaberi QR kao sliku.');
    }
    if (e.type === 'image') {
      if (!Number.isInteger(e.imageWidth) || !number(e.imageWidth, 8, 576) || !Number.isInteger(e.threshold) || !number(e.threshold, 1, 255)) fail('Neispravna veličina ili prag slike.');
      if (e.image !== null) {
        const im = e.image;
        if (!im || !Number.isInteger(im.width) || !Number.isInteger(im.height) || !number(im.width, 1, 256) || !number(im.height, 1, 256) ||
            typeof im.gray !== 'string' || im.gray.length !== im.width * im.height * 2 || !/^[0-9a-f]+$/i.test(im.gray)) fail('Neispravni pikseli slike. Ponovo učitaj logo.');
      }
    }
    if (e.type === 'table') {
      if (typeof e.source !== 'string' || !Array.isArray(e.columns) || !number(e.columns.length, 1, 6)) fail('Tabela mora imati 1–6 kolona.');
      if (Math.abs(e.columns.reduce((sum, c) => sum + c.width, 0) - 100) > 0.01) fail('Širine kolona moraju ukupno iznositi 100%.');
      for (const c of e.columns) {
        if (typeof c.key !== 'string' || typeof c.label !== 'string' || c.label.length > 100 || !number(c.width, 5, 100) ||
            !['left', 'right', 'center'].includes(c.align)) fail('Neispravna kolona tabele.');
      }
    }
  }
  if (JSON.stringify(t).length > 750000) fail('Predložak je prevelik. Smanji broj ili veličinu slika.');
  return t;
}
function valueAt(data, path) {
  let value = data;
  for (const key of path.trim().split('.')) {
    if (!value || typeof value !== 'object' || !Object.hasOwn(value, key)) fail(`Nedostaje podatak: ${path}`);
    value = value[key];
  }
  return value;
}
function scalar(value) {
  if (!['string', 'number', 'boolean'].includes(typeof value)) fail('Ćelija ili tekstualno polje mora biti jednostavna vrijednost.');
  const text = String(value).normalize('NFC');
  if (text.length > 10000) fail('Vrijednost je preduga.');
  if (/[\x00-\x09\x0b-\x1f\x7f]/u.test(text)) fail('Kontrolni znakovi nisu dozvoljeni u tekstu.');
  return text;
}
export function bind(text, data) {
  return scalar(text.replace(/\{\{\s*([^{}]+?)\s*\}\}/g, (_, path) => scalar(valueAt(data, path))));
}
export function encodeText(text) {
  return [...text.normalize('NFC')].map(char => {
    const code = char.codePointAt(0);
    if (code >= 32 && code < 127) return code;
    const index = cp852.indexOf(char);
    if (index < 0) fail(`Znak „${char}“ nije podržan u CP852.`);
    return index + 128;
  });
}
// Element geometry uses percentages. RPE resolves them onto Font A
// character columns; preview is decoded from the bytes it actually produces.
export function gridGeometry(template, element) {
  const profile = profiles[template.profile];
  const columns = profile.width / profile.charWidth;
  const start = Math.min(Math.floor(columns * .9), Math.round(columns * element.x / 100));
  const span = Math.max(1, Math.min(columns - start, Math.round(columns * element.width / 100)));
  return { columns, start, span, x: start * profile.charWidth, width: span * profile.charWidth };
}

export function compile(template, data, { preview: includePreview = true } = {}) {
  validateTemplate(template);
  if (!data || typeof data !== 'object' || Array.isArray(data)) fail('Podaci moraju biti JSON objekt.');
  const profile = profiles[template.profile];
  const language = template.language || 'esc-pos';
  const star = language !== 'esc-pos';
  const encoder = new ReceiptPrinterEncoder({
    language, columns: profile.width / profile.charWidth,
    codepageMapping: { cp852: template.codePage }, newline: '\n', feedBeforeCut: 0,
  });
  const preview = new EscPosPreview(profile, template.codePage, language, { capture: includePreview });
  const bytes = [], commands = [], blocks = [];
  const append = (chunk, geometry) => {
    const decoded = preview.read(chunk, geometry);
    for (const command of decoded.commands) commands.push({ ...command, offset: bytes.length + command.offset });
    for (const byte of chunk) bytes.push(byte);
    return decoded.block;
  };
  encoder.initialize();
  if (star) {
    // Star: font A, line mode, zero left margin, right margin in characters,
    // 4 mm line pitch. Do not send Epson motion-unit or area commands.
    encoder.font('A');
    if (language === 'star-prnt') encoder.raw([27, 29, 80, 49]);
    encoder.raw([27, 108, 0, 27, 81, profile.width / profile.charWidth, 27, 122, 1]);
  } else {
    if (template.outputTarget !== 'virtual-printer') encoder.raw([27, 83, 29, 80, 203, 203]);
    encoder.raw([29, 76, 0, 0, 29, 87, profile.width & 255, profile.width >> 8]);
  }
  append(encoder.encode());
  encoder.codepage('cp852');
  const checked = value => {
    const text = scalar(value);
    // RPE substitutes unsupported characters; our editor deliberately rejects
    // them before they reach the library so amounts/names cannot silently change.
    for (const paragraph of text.split('\n')) encodeText(paragraph);
    return text;
  };
  for (const element of template.elements) {
    const grid = gridGeometry(template, element);
    const styled = (text, header = false) => cell => {
      cell.width(element.scale).height(element.scale)
        .bold(header || element.bold).underline(element.underline).text(text);
    };
    if (!star) encoder.raw([27, 51, profile.charHeight * element.scale + 6]);
    if (element.type === 'text') {
      if (grid.span < element.scale) fail('Element je preuski za odabrani font.');
      encoder.table(
        [{ width: grid.span, marginLeft: grid.start, align: element.align }],
        [[styled(checked(bind(element.text, data)))]],
      );
    } else if (element.type === 'table') {
      const rows = valueAt(data, element.source);
      if (!Array.isArray(rows) || rows.length > 500) fail('Izvor tabele mora biti niz s najviše 500 stavki.');
      let accumulated = 0;
      const columns = element.columns.map((column, i) => {
        const start = Math.floor(grid.span * accumulated / 100);
        accumulated += column.width;
        const end = i === element.columns.length - 1 ? grid.span : Math.floor(grid.span * accumulated / 100);
        const gutter = i < element.columns.length - 1 ? 1 : 0;
        const width = end - start - gutter;
        if (width < element.scale) fail('Element ili kolona su preuski za odabrani font.');
        return { width, align: column.align, marginLeft: i === 0 ? grid.start : 0, marginRight: gutter };
      });
      const values = [
        element.columns.map(column => styled(checked(column.label), true)),
        ...rows.map(row => element.columns.map(column => styled(checked(valueAt(row, column.key))))),
      ];
      encoder.table(columns, values);
    } else {
      const value = element.type === 'qr' ? bind(element.content, data) : '';
      if (element.type === 'qr' && (!value.trim() || value.length > 1000)) fail('QR sadržaj mora imati 1–1000 znakova.');
      if (element.type === 'qr' && element.qrMode === 'native') {
        if (/[^\x20-\x7e]/.test(value)) fail('Nativni QR podržava ASCII sadržaj (npr. URL). Za čćžšđ i drugi Unicode odaberi QR kao sliku.');
        const matrix = qrMatrix(value, element.errorLevel, true);
        if ((matrix.size + 8) * element.moduleSize > profile.width) fail('QR kod je preširok. Smanji veličinu modula ili sadržaj.');
        // Leave four modules above/below and at the paper edges. Device QR
        // encoding may select a different mask/segmentation than the preview.
        const quiet = element.moduleSize * 4;
        encoder.raw(star ? [27, 73, quiet] : [27, 74, quiet]);
        if (!star) encoder.raw([29, 76, quiet, 0, 29, 87, (profile.width - quiet * 2) & 255, (profile.width - quiet * 2) >> 8]);
        else encoder.raw([27, 108, Math.ceil(quiet / 12), 27, 81, profile.width / 12 - Math.ceil(quiet / 12)]);
        encoder.align(element.align).qrcode(value, { model: 2, size: element.moduleSize, errorlevel: element.errorLevel }).align('left');
        if (!star) encoder.raw([29, 76, 0, 0, 29, 87, profile.width & 255, profile.width >> 8]);
        else encoder.raw([27, 108, 0, 27, 81, profile.width / 12]);
        encoder.raw(star ? [27, 73, quiet] : [27, 74, quiet]);
      } else {
        const image = graphicImage(element, grid, profile.width, value);
        printGraphic(encoder, image, star);
      }
    }
    // The library owns every text/column/style command. These small raw
    // commands retain our existing per-element vertical spacing.
    const gap = Math.round(element.gap);
    // Star ESC I feeds n/8 mm (one dot on the supported 8-dot/mm thermal
    // profiles); ESC J on Star is n/4 mm, not the Epson dot feed.
    if (star) { if (gap) encoder.raw([27, 73, gap]); }
    else encoder.raw([27, 74, gap]);
    const block = append(encoder.encode(), { id: element.id, x: grid.x, width: grid.width });
    if (includePreview) blocks.push(block);
  }
  const contentHeight = preview.y;
  if (star) encoder.raw([27, 122, 1]).newline(3);
  else encoder.raw([27, 51, 30]).newline(3);
  if (template.cut) encoder.cut('partial');
  append(encoder.encode());
  if (!includePreview) return { bytes: Uint8Array.from(bytes) };
  const height = preview.cutY ?? preview.y;
  const plan = { profile, blocks, width: profile.width, height, tailHeight: height - contentHeight, engine: 'rpe-4.0.1', language };
  return { plan, bytes: Uint8Array.from(bytes), commands };
}
export function layout(template, data) {
  return compile(template, data).plan;
}
