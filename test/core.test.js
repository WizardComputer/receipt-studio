import test from 'node:test';
import assert from 'node:assert/strict';
import { compile, layout, initialTemplate, sampleData, encodeText, validateTemplate, profiles, gridGeometry } from '../src/core.js';

test('new receipts feed 20 dots before the company name without an extra blank line', () => {
  for (const language of ['esc-pos', 'star-prnt', 'star-line']) {
    const t = initialTemplate();
    t.language = language;
    t.codePage = language === 'esc-pos' ? 18 : 5;
    const r = compile(t, sampleData);
    assert.equal(t.elements[0].text, '');
    assert.equal(t.elements[0].width, 100);
    assert.equal(r.plan.blocks[0].height, 20);
    assert.equal(r.plan.blocks[0].lines.length, 0);
    assert.equal(r.plan.blocks.find(b => b.id === 'brand').y, 20);
    const feed = r.commands.find(c => c.name.startsWith(language === 'esc-pos' ? 'ESC J' : 'ESC I'));
    assert.deepEqual(feed.bytes, [27, language === 'esc-pos' ? 74 : 73, 20]);
  }
});

test('Croatian/Bosnian characters produce CP852 bytes, including decomposed Unicode', () => {
  assert.deepEqual(encodeText('čćžšđ ČĆŽŠĐ'), [159, 134, 167, 231, 208, 32, 172, 143, 166, 230, 209]);
  assert.deepEqual(encodeText('c\u030c'), [159]);
  assert.throws(() => encodeText('🙂'), /CP852/);
});
test('RPE wraps paragraphs and long words before preview decodes them', () => {
  const template = initialTemplate();
  template.elements = [{...template.elements.find(e=>e.id==='brand'),text:'Domaća kafa\nABCDEFGHI',scale:1,width:12.5,align:'left',gap:0}];
  const block = compile(template,{}).plan.blocks[0];
  assert.deepEqual(block.lines.map(line=>line.runs.map(run=>run.text).join('')), ['Domaća','kafa','ABCDEF','GHI']);
});
test('both printer widths keep every text run inside its block and every table cell separate', () => {
  for (const profile of ['wide', 'narrow']) {
    const t = initialTemplate(); t.profile = profile;
    const data = structuredClone(sampleData);
    data.items.push({ name: 'Vrlo dugačak naziv artikla koji se mora prelomiti u više redova', qty: '123456', price: '123456789,00' });
    const plan = layout(t, data);
    for (const block of plan.blocks) {
      for (const line of block.lines) {
        let right = block.x;
        for (const run of line.runs) {
          assert.ok(run.x >= right);
          assert.ok(run.x + run.width <= block.x + block.width);
          right = run.x + run.width;
        }
      }
    }
  }
});
test('adding wrapped table rows pushes the total below all rows', () => {
  const t = initialTemplate();
  const before = layout(t, sampleData);
  const data = structuredClone(sampleData);
  data.items.push({ name: 'Dugačak naziv '.repeat(12), qty: '1', price: '1,00' });
  const after = layout(t, data);
  const table = after.blocks.find(b => b.id === 'items');
  const total = after.blocks.find(b => b.id === 'total');
  assert.equal(total.y, table.y + table.height);
  assert.ok(total.y > before.blocks.find(b => b.id === 'total').y);
});
test('empty table retains headers and allows a following total', () => {
  const data = { ...sampleData, items: [] };
  const plan = layout(initialTemplate(), data);
  assert.equal(plan.blocks.find(b => b.id === 'items').lines.length, 1);
});
test('RPE bytes retain CP852, styles, explicit spacing and cut; preview reads them', () => {
  const t = initialTemplate();
	assert.equal(t.cut, true);
	t.cut = false;
  t.elements = [{ ...t.elements.find(e=>e.id==='brand'), text: 'ČĆ', x: 10, width: 80, align: 'right', bold: true, underline: true, scale: 2, gap: 12 }];
  const result = compile(t, {});
  const commands = name => result.commands.filter(c => c.name.startsWith(name)).map(c => c.bytes);
  assert.ok(commands('ESC t').some(bytes => bytes.join() === '27,116,18'));
  assert.equal(commands('ESC $').length,0);
  const run=result.plan.blocks[0].lines[0].runs.find(run=>run.text.includes('ČĆ'));
  assert.equal(run.charWidth,24);assert.equal(run.charHeight,48);
  assert.equal(run.bold,true);assert.equal(run.underline,true);
  assert.ok(commands('TEXT').some(bytes=>bytes.includes(172)&&bytes.includes(143)));
  assert.deepEqual(commands('ESC 3')[0],[27,51,54]);
  assert.deepEqual(commands('ESC J'),[[27,74,12]]);
  assert.equal(result.plan.blocks[0].height,66);
  assert.equal(result.plan.tailHeight,90);
  assert.deepEqual(result.bytes, Uint8Array.from(result.commands.flatMap(c => c.bytes)));
  assert.equal(commands('GS V').length,0);
  t.cut=true;
  assert.deepEqual(compile(t,{}).commands.find(c=>c.name.startsWith('GS V')).bytes,[29,86,1]);
  assert.equal(compile(t,{}).plan.tailHeight,90);
});
test('invalid or missing data fails explicitly instead of printing silent replacements', () => {
  const t = initialTemplate();
  assert.throws(() => compile(t, {}), /Nedostaje podatak/);
  assert.throws(() => compile(t, { ...sampleData, items: {} }), /niz/);
  assert.throws(() => compile(t, { ...sampleData, shop: '\x1b@' }), /Kontrolni/);
  assert.throws(() => compile(t, { ...sampleData, shop: '🙂' }), /CP852/);
});
test('untrusted templates cannot inject bytes through numeric fields or control text', () => {
  const t = initialTemplate(); t.codePage = -1;
  assert.throws(() => validateTemplate(t));
  t.codePage = 18; t.elements.find(e=>e.id==='brand').scale = 99;
  assert.throws(() => validateTemplate(t));
  t.elements.find(e=>e.id==='brand').scale = 1; t.elements.find(e=>e.id==='brand').text = 'Hello\x1dV';
  assert.throws(() => compile(t, sampleData), /Kontrolni/);
  t.elements.find(e=>e.id==='brand').text = 'Hello'; t.elements.find(e=>e.id==='address').id = t.elements.find(e=>e.id==='brand').id;
  assert.throws(() => validateTemplate(t), /dupli/);
});
test('rejects oversized jobs and table columns narrower than a character', () => {
  assert.throws(() => compile(initialTemplate(), { ...sampleData, items: Array(501).fill(sampleData.items[0]) }), /500/);
  const t = initialTemplate(); const table = t.elements.find(e => e.type === 'table');
  table.width = 10;
  assert.throws(() => compile(t, sampleData), /preuski/);
});

test('Virtual Printer export avoids unsupported setup while preserving layout and CP852', () => {
  const template = initialTemplate();
  const physical = compile(template, sampleData);
  template.outputTarget = 'virtual-printer';
  const virtual = compile(template, sampleData);
  assert.equal(virtual.commands.some(c => c.name.startsWith('ESC S')), false);
  assert.equal(virtual.commands.some(c => c.name.startsWith('GS P')), false);
  assert.deepEqual(virtual.plan, physical.plan);
  assert.deepEqual(virtual.bytes, Uint8Array.from(physical.commands
    .filter(c => !c.name.startsWith('ESC S') && !c.name.startsWith('GS P'))
    .flatMap(c => c.bytes)));
  assert.deepEqual(virtual.commands.find(c => c.name.startsWith('ESC t')).bytes, [27, 116, 18]);
  assert.ok(physical.commands.some(c => c.name.startsWith('GS P')));
});

test('output target is optional for existing templates and rejects unknown targets', () => {
  const template = initialTemplate();
  delete template.outputTarget;
  assert.doesNotThrow(() => validateTemplate(template));
  template.outputTarget = 'unknown';
  assert.throws(() => validateTemplate(template), /izvoza/);
});

test('centering uses RPE character padding: ABC starts at 264, not 270 dots', () => {
  const t=initialTemplate();
  t.elements=[{...t.elements.find(e=>e.id==='brand'),text:'ABC',scale:1,width:100,gap:0}];
  const r=compile(t,{});
  assert.equal(r.plan.blocks[0].lines[0].runs[0].x,264);
  assert.equal(r.plan.blocks[0].lines[0].runs[0].text,'ABC');
  const text=r.commands.filter(c=>c.name.startsWith('TEXT')).flatMap(c=>c.bytes);
  assert.equal(text.indexOf(65),22);
});
test('percentage geometry resolves to whole columns without modifying saved templates', () => {
  const t=initialTemplate();const e=t.elements.find(e=>e.id==='brand');e.x=13;e.width=77;
  const before=JSON.stringify(t),g=gridGeometry(t,e);
  assert.equal(g.x,72);assert.equal(g.width,444);
  compile(t,sampleData);assert.equal(JSON.stringify(t),before);
});
test('all alignments and font sizes stay inside snapped block bounds', () => {
  for (const profile of ['wide','narrow']) for (const scale of [1,2]) for (const align of ['left','center','right']) {
    const t=initialTemplate();t.profile=profile;
    t.elements=[{...t.elements.find(e=>e.id==='brand'),text:'ČĆ ŽŠĐ\nDugi naziv artikla '.repeat(3),scale,align,x:13,width:77}];
    const r=compile(t,{}),block=r.plan.blocks[0];
    for (const line of block.lines) for (const run of line.runs) {
      assert.ok(run.x>=block.x);
      assert.ok(run.x+run.width<=block.x+block.width);
      assert.equal(run.x%12,0);
    }
  }
});
test('CP852 selection respects the configured printer page', () => {
  const t=initialTemplate();t.codePage=26;
  const r=compile(t,sampleData);
  assert.ok(r.commands.filter(c=>c.name.startsWith('ESC t')).every(c=>c.bytes[2]===26));
});
test('actual RPE text bytes preserve all local letters and normalize decomposed input', () => {
  const t=initialTemplate();
  t.elements=[{...t.elements.find(e=>e.id==='brand'),scale:1,align:'left',text:'čćžšđ ČĆŽŠĐ c\u030c',gap:0}];
  const r=compile(t,{});
  const bytes=r.commands.filter(c=>c.name.startsWith('TEXT')).flatMap(c=>c.bytes);
  const expected=[159,134,167,231,208,32,172,143,166,230,209,32,159];
  assert.deepEqual(bytes.slice(0,expected.length),expected);
  assert.equal(r.plan.blocks[0].lines[0].runs[0].text,'čćžšđ ČĆŽŠĐ č');
});
