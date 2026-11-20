import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
test('browser/Goja dependency is the unchanged pinned upstream RPE distribution', async () => {
  const base = new URL('../', import.meta.url);
  const [local, upstream, manifest] = await Promise.all([
    readFile(new URL('src/vendor/receipt-printer-encoder-4.0.1.js',base)),
    readFile(new URL('node_modules/@point-of-sale/receipt-printer-encoder/dist/receipt-printer-encoder.umd.js',base)),
    readFile(new URL('node_modules/@point-of-sale/receipt-printer-encoder/package.json',base),'utf8'),
  ]);
  assert.equal(JSON.parse(manifest).version,'4.0.1');
  assert.deepEqual(local,upstream);
});
