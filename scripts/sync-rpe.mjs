// Copy an already-published dependency; this is not an application build.
import { readFile, writeFile, copyFile, mkdir } from 'node:fs/promises';
const root = new URL('../', import.meta.url);
const directory = new URL('src/vendor/', root);
const dependency = new URL('node_modules/@point-of-sale/receipt-printer-encoder/', root);
const info = JSON.parse(await readFile(new URL('package.json', dependency), 'utf8'));
if (info.version !== '4.0.1') throw new Error('Review the adapter and preview before upgrading RPE.');
await mkdir(directory, { recursive: true });
await copyFile(new URL('dist/receipt-printer-encoder.umd.js', dependency), new URL('receipt-printer-encoder-4.0.1.js', directory));
await copyFile(new URL('LICENSE', dependency), new URL('ReceiptPrinterEncoder.LICENSE', directory));
const dependencies = ['@point-of-sale/receipt-printer-encoder', '@point-of-sale/codepage-encoder', '@canvas/image-data', 'canvas-dither', 'canvas-flatten', 'resize-image-data'];
let notices = 'Locally bundled ReceiptPrinterEncoder 4.0.1 and its dependencies.\n\n';
for (const name of dependencies) {
  const path = new URL('node_modules/' + name + '/', root);
  const metadata = JSON.parse(await readFile(new URL('package.json', path), 'utf8'));
  notices += name + '@' + metadata.version + '\nLicense: ' + metadata.license + '\n';
  if (metadata.repository) notices += JSON.stringify(metadata.repository) + '\n';
  for (const file of ['LICENSE', 'LICENSE.md', 'LICENSE.txt']) {
    try { notices += await readFile(new URL(file,path),'utf8'); break; } catch(e) { if(e.code !== 'ENOENT') throw e; }
  }
  notices += '\n\n';
}
await writeFile(new URL('THIRD_PARTY_NOTICES.txt',directory),notices);

// QR preview and optional isolated QR raster, no browser/Node API dependencies.
const qrDependency = new URL('node_modules/qrcode-generator/', root);
const qrInfo = JSON.parse(await readFile(new URL('package.json', qrDependency), 'utf8'));
if (qrInfo.version !== '2.0.4') throw new Error('Review QR output before upgrading.');
const qrSource = await readFile(new URL('dist/qrcode.js', qrDependency), 'utf8');
await writeFile(new URL('qrcode-generator-2.0.4.js', directory), qrSource + '\n// Expose the unchanged library API to browser modules and Goja.\nglobalThis.ReceiptQRCode = qrcode;\n');
await writeFile(new URL('QRCodeGenerator.NOTICE.txt', directory), 'qrcode-generator@2.0.4\nCopyright (c) 2009 Kazuhiko Arase\nMIT license; copyright and license URL retained in source.\nhttps://github.com/kazuhikoarase/qrcode-generator\nOnly modification: globalThis.ReceiptQRCode export appended.\n');
