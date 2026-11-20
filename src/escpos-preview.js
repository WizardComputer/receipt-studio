import { cp852 } from './cp852.js';
import { qrMatrix, packedPixels } from './graphics.js';

// Read the actual printer bytes. This interpreter never wraps, aligns or
// measures browser fonts; unsupported commands fail instead of guessing.
export class EscPosPreview {
  constructor(profile, codePage, language = 'esc-pos', { capture = true, commands = capture } = {}) {
    // Validation-only mode keeps cursor/bounds checks without allocating a
    // drawing, decoded text, graphic pixels or command descriptions.
    this.capture = capture;
    this.captureCommands = commands;
    this.language = language;
    this.star = language !== 'esc-pos';
    this.profile = profile;
    this.codePage = codePage;
    this.y = 0;
    this.reset();
  }
  reset() {
    this.x = 0; this.scaleX = 1; this.scaleY = 1;
    this.bold = false; this.underline = false; this.spacing = this.star ? 32 : 30;
    this.cutY = null; this.activeCodePage = null; this.printedHeight = 0; this.line = null;
    this.align = 0; this.margin = 0; this.area = this.profile.width;
    this.qr = { size: 4, level: 'm', value: '' };
  }
  read(bytes, { id = '', x = 0, width = this.profile.width } = {}) {
    const block = this.capture ? { id, x, y: this.y, width, height: 0, lines: [] } : null;
    const commands = [];
    let offset = 0;
    const requireBytes = count => {
      if (offset + count > bytes.length) throw new Error('Nepotpuna komanda printera.');
    };
    const advance = amount => {
      if (this.capture && this.line) this.line.height = amount;
      this.y += amount; this.x = 0; this.line = null; this.printedHeight = 0;
      if (this.y > 100000) throw new Error('Račun je predug.');
    };
    const drawGraphic = (w, h, data, immediate = true, kind = 'image') => {
      if (w < 1 || h < 1 || w > this.area || h > 768) throw new Error('Grafika prelazi područje za printanje.');
      if (this.capture) {
        const gx = this.margin + (this.align === 2 ? this.area - w : this.align === 1 ? Math.floor((this.area - w) / 2) : 0);
        (block.graphics ||= []).push({ x: gx, y: this.y, width: w, height: h, data: Array.from(data), kind });
      }
      if (immediate) advance(h);
      else this.printedHeight = Math.max(this.printedHeight, h);
    };
    const printQR = () => {
      if (!this.qr.value) throw new Error('QR nema sadržaj.');
      const matrix = qrMatrix(this.qr.value, this.qr.level, true), w = matrix.size * this.qr.size;
      if (!this.capture) { drawGraphic(w, w, null, true, 'native-qr'); return; }
      const pixels = Array.from({ length: w * w }, (_, i) => matrix.pixels[Math.floor(Math.floor(i / w) / this.qr.size) * matrix.size + Math.floor((i % w) / this.qr.size)]);
      drawGraphic(w, w, packedPixels(w, w, pixels), true, 'native-qr');
    };
    while (offset < bytes.length) {
      const start = offset, b = bytes[offset];
      let count = 1, name;
      if (!this.star && b === 29 && bytes[offset + 1] === 118) {
        requireBytes(8);
        if (bytes[offset + 2] !== 48 || bytes[offset + 3] !== 0) throw new Error('Nepodržana raster komanda.');
        const stride = bytes[offset + 4] + bytes[offset + 5] * 256, h = bytes[offset + 6] + bytes[offset + 7] * 256;
        count = 8 + stride * h; requireBytes(count);
        drawGraphic(stride * 8, h, this.capture ? bytes.slice(offset + 8, offset + count) : null);
        name = 'GS v 0 · raster slike';
      } else if (this.star && b === 27 && bytes[offset + 1] === 88) {
        requireBytes(4);
        const w = bytes[offset + 2] + bytes[offset + 3] * 256;
        count = 4 + w * 3; requireBytes(count);
        if (w < 1 || w > this.area) throw new Error('Grafika prelazi područje za printanje.');
        const stride = Math.ceil(w / 8), packed = this.capture ? new Uint8Array(stride * 24) : null;
        if (this.capture) for (let x = 0; x < w; x++) for (let section = 0; section < 3; section++) {
          const value = bytes[offset + 4 + x * 3 + section];
          if (!value) continue;
          for (let bit = 0; bit < 8; bit++) if (value & (128 >> bit)) packed[(section * 8 + bit) * stride + (x >> 3)] |= 128 >> (x & 7);
        }
        drawGraphic(w, 24, packed, false);
        name = 'ESC X · Star slika, 24 reda';
      } else if (!this.star && b === 29 && bytes[offset + 1] === 40) {
        requireBytes(8);
        count = 5 + bytes[offset + 3] + bytes[offset + 4] * 256; requireBytes(count);
        if (bytes[offset + 2] !== 107 || bytes[offset + 5] !== 49 || count < 8) throw new Error('Nepodržana QR komanda.');
        const fn = bytes[offset + 6], n = bytes[offset + 7];
        if (fn === 65) { if (count !== 9 || n !== 50) throw new Error('Podržan je samo QR model 2.'); }
        else if (fn === 67) this.qr.size = n;
        else if (fn === 69) this.qr.level = ['l','m','q','h'][n - 48];
        else if (fn === 80) this.qr.value = Array.from(bytes.slice(offset + 8, offset + count), v => String.fromCharCode(v)).join('');
        else if (fn === 81) printQR();
        else throw new Error('Nepodržana QR funkcija.');
        name = 'GS ( k · QR ' + fn;
      } else if (this.star && b === 27 && bytes[offset + 1] === 29 && bytes[offset + 2] === 121) {
        requireBytes(4);
        const fn = bytes[offset + 3];
        if (fn === 83) {
          count = 6; requireBytes(count);
          const property = bytes[offset + 4], n = bytes[offset + 5];
          if (property === 48) { if (n !== 2) throw new Error('Podržan je samo QR model 2.'); }
          else if (property === 50) this.qr.size = n;
          else if (property === 49) this.qr.level = ['l','m','q','h'][n];
          else throw new Error('Nepodržana Star QR postavka.');
        } else if (fn === 68) {
          requireBytes(8); count = 8 + bytes[offset + 6] + bytes[offset + 7] * 256; requireBytes(count);
          if (bytes[offset + 4] !== 49 || bytes[offset + 5] !== 0) throw new Error('Nepodržani Star QR podaci.');
          this.qr.value = Array.from(bytes.slice(offset + 8, offset + count), v => String.fromCharCode(v)).join('');
        } else if (fn === 80) { count = 4; printQR(); }
        else throw new Error('Nepodržana Star QR funkcija.');
        name = 'ESC GS y · Star QR';
      } else if (this.star && b === 27) {
        requireBytes(2);
        const command = bytes[offset + 1];
        const sizes = {64:2,69:2,70:2,45:3,105:4,100:3,122:3,73:3,74:3,97:3,108:3,81:3,48:2,29:4,30:4};
        count = sizes[command];
        if (!count) throw new Error('Pregled ne podržava Star komandu ESC ' + command);
        requireBytes(count);
        const n = bytes[offset + 2];
        switch (command) {
          case 64: this.reset(); name = 'ESC @ · Star inicijalizacija'; break;
          case 69: this.bold = true; name = 'ESC E · Star podebljanje'; break;
          case 70: this.bold = false; name = 'ESC F · Star normalni tekst'; break;
          case 45: this.underline = n !== 0; name = 'ESC - · podvlačenje'; break;
          case 105:
            this.scaleY = n + 1; this.scaleX = bytes[offset + 3] + 1;
            name = 'ESC i · Star veličina teksta'; break;
          case 100: this.cutY = this.y; name = 'ESC d · Star rezanje'; break;
          case 122:
            if (n !== 0 && n !== 1) throw new Error('Nepoznat Star razmak reda.');
            this.spacing = n === 1 ? 32 : 24; name = 'ESC z · Star visina reda'; break;
          case 48: this.spacing = 24; name = 'ESC 0 · Star 3 mm'; break;
          case 73: advance(n); name = 'ESC I · Star pomak n/8 mm'; break;
          case 74: advance(n * 2); name = 'ESC J · Star pomak n/4 mm'; break;
          case 97: advance(n * this.spacing); name = 'ESC a · Star pomak redova'; break;
          case 108:
            this.margin = n * this.profile.charWidth;
            name = 'ESC l · Star lijeva margina'; break;
          case 81:
            this.area = n * this.profile.charWidth - this.margin;
            name = 'ESC Q · Star desna margina'; break;
          case 30:
            if (n !== 70 || bytes[offset + 3] !== 0) throw new Error('Pregled podržava samo Star font A.');
            name = 'ESC RS F · Star font A'; break;
          case 29:
            if (n === 116) {
              if (bytes[offset + 3] !== this.codePage) throw new Error('Neočekivana kodna stranica.');
              this.activeCodePage = bytes[offset + 3]; name = 'ESC GS t · Star CP852';
            } else if (n === 97) {
              this.align = bytes[offset + 3]; name = 'ESC GS a · Star poravnanje';
            } else if (n === 80 && [48,49].includes(bytes[offset + 3])) {
              name = 'ESC GS P · Star pražnjenje buffera / režim';
            } else throw new Error('Nepodržana Star ESC GS komanda.');
            break;
        }
      } else if (this.star && b === 24) {
        if (this.line) throw new Error('Star CAN prekida nepotpun red.');
        this.x = 0; name = 'CAN · Star isprazni red';
      } else if (!this.star && (b === 27 || b === 29 || b === 28)) {
        requireBytes(2);
        const command = bytes[offset + 1];
        const key = b + ':' + command;
        const sizes = {'27:97':3,'27:64':2,'27:83':2,'27:50':2,'28:46':2,'27:77':3,'27:116':3,'27:69':3,'27:45':3,'27:51':3,'27:74':3,'27:100':3,'29:33':3,'29:80':4,'29:76':4,'29:87':4,'29:86':3};
        count = sizes[key];
        if (!count) throw new Error('Pregled ne podržava ESC/POS komandu ' + key);
        requireBytes(count);
        const n = bytes[offset + 2];
        switch (key) {
          case '27:64': this.reset(); name = 'ESC @ · RPE inicijalizacija'; break;
          case '27:97': this.align = n; name = 'ESC a · poravnanje grafike'; break;
          case '27:83': name = 'ESC S · standard mode'; break;
          case '28:46': name = 'FS . · jednobajtni tekst'; break;
          case '27:50': this.spacing = 30; name = 'ESC 2 · standardni razmak'; break;
          case '27:77':
            if (n !== 0) throw new Error('Pregled podržava samo font A.');
            name = 'ESC M · font A'; break;
          case '27:116':
            if (n !== this.codePage) throw new Error('Neočekivana kodna stranica.');
            this.activeCodePage = n; name = 'ESC t · CP852'; break;
          case '27:69': this.bold = n !== 0; name = 'ESC E · podebljanje'; break;
          case '27:45': this.underline = n !== 0; name = 'ESC - · podvlačenje'; break;
          case '27:51': this.spacing = n; name = 'ESC 3 · visina reda'; break;
          case '27:74': advance(n); name = 'ESC J · pomak papira'; break;
          case '27:100': advance(n * this.spacing); name = 'ESC d · završni pomak'; break;
          case '29:33':
            this.scaleX = (n >> 4) + 1; this.scaleY = (n & 15) + 1;
            name = 'GS ! · veličina teksta'; break;
          case '29:80': name = 'GS P · jedinice pozicioniranja'; break;
          case '29:76': this.margin = n + bytes[offset + 3] * 256; name = 'GS L · lijeva margina'; break;
          case '29:87': this.area = n + bytes[offset + 3] * 256; name = 'GS W · širina printanja'; break;
          case '29:86': this.cutY = this.y; name = 'GS V · rezanje'; break;
        }
      } else if (b === 10) {
        if (this.capture && !this.line) {
          this.line = { y: this.y, height: 0, runs: [] };
          block.lines.push(this.line);
        }
        advance(Math.max(this.spacing, this.printedHeight));
        name = 'LF · novi red';
      } else if (b === 13) {
        this.x = 0; name = 'CR · početak reda';
      } else if (b >= 32 && b !== 127) {
        while (offset + count < bytes.length && bytes[offset + count] >= 32 && bytes[offset + count] !== 127) count++;
        if (this.activeCodePage === null) {
          for (let i = offset; i < offset + count; i++) if (bytes[i] >= 128) throw new Error('Kodna stranica nije postavljena.');
        }
        const cw = this.profile.charWidth * this.scaleX, ch = this.profile.charHeight * this.scaleY;
        const printedWidth = count * cw;
        if (this.x + printedWidth > this.profile.width) throw new Error('RPE tekst prelazi širinu printera.');
        if (this.capture) {
          const text = Array.from(bytes.slice(offset, offset + count), v => v < 128 ? String.fromCharCode(v) : cp852[v - 128]).join('');
          if (!this.line) {
            this.line = { y: this.y, height: 0, runs: [] };
            block.lines.push(this.line);
          }
          // Spaces still advance the actual printer cursor. Only skip their
          // invisible glyphs; underlined spaces must remain in the preview.
          const leading = this.underline ? 0 : text.length - text.trimStart().length;
          const visible = this.underline ? text : text.trim();
          if (visible) this.line.runs.push({
            text: visible, x: this.x + leading * cw, width: visible.length * cw,
            charWidth: cw, charHeight: ch, scale: this.scaleX,
            bold: this.bold, underline: this.underline,
          });
          name = 'TEXT · ' + text;
        } else {
          this.line = true;
        }
        this.x += printedWidth;
        this.printedHeight = Math.max(this.printedHeight, ch);
      } else {
        throw new Error('Pregled ne podržava kontrolni bajt ' + b);
      }
      if (this.captureCommands) commands.push({ name, offset: start, bytes: Array.from(bytes.slice(start, start + count)) });
      offset += count;
    }
    if (this.line) throw new Error('RPE blok nije završen novim redom.');
    if (block) block.height = (this.cutY ?? this.y) - block.y;
    return { block, commands };
  }
}
