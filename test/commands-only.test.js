import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { compile, initialTemplate, profiles } from '../src/core.js';
import { EscPosPreview } from '../src/escpos-preview.js';

const fixtures = JSON.parse(readFileSync(new URL('../testdata/encoder.json', import.meta.url)));
for (const fixture of fixtures) {
  test(`commands only preserves frozen bytes: ${fixture.name}`, () => {
    const result = compile(fixture.template, fixture.data, { preview: false });
    assert.equal(Buffer.from(result.bytes).toString('hex'), fixture.hex);
    assert.equal(result.plan, undefined);
    assert.equal(result.commands, undefined);
    const language = fixture.template.language || 'esc-pos';
    const read = options => new EscPosPreview(profiles[fixture.template.profile], fixture.template.codePage, language, options).read(result.bytes);
    assert.deepEqual(read({ commands: false }).block, read().block);
    assert.deepEqual(read({ commands: false }).commands, []);
  });
}

test('commands only still rejects excessive height and unsupported characters', () => {
  const template = initialTemplate();
  template.elements = [{ ...template.elements[0], text: '\n'.repeat(3500) }];
  for (const preview of [false, true]) {
    assert.throws(() => compile(template, {}, { preview }), /predug/);
  }
  template.elements[0].text = '🙂';
  assert.throws(() => compile(template, {}, { preview: false }), /CP852/);
});

test('validation-only decoder preserves byte, width, code-page and graphics checks', () => {
  for (const bytes of [
    [27], [1], [128, 10], [...Array(49).fill(65), 10], [65],
    [29, 118, 48, 0, 73, 0, 1, 0, ...Array(73).fill(0)],
    [27, 100, 255, ...Array(13).fill([27, 100, 255]).flat()],
  ]) {
    const read = capture => new EscPosPreview(profiles.wide, 18, 'esc-pos', { capture }).read(Uint8Array.from(bytes));
    let expected;
    try { read(true); } catch (error) { expected = error.message; }
    assert.ok(expected);
    assert.throws(() => read(false), { message: expected });
  }
});
