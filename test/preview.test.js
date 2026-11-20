import test from 'node:test';
import assert from 'node:assert/strict';
import { EscPosPreview } from '../src/escpos-preview.js';
import { compile, initialTemplate, sampleData, profiles } from '../src/core.js';
import { ReceiptPrinterEncoder } from '../src/rpe-runtime.js';

test('preview reads upstream RPE output, including padding and wrapped table cells', () => {
  const encoder=new ReceiptPrinterEncoder({columns:32,codepageMapping:{cp852:18},newline:'\n'});
  const bytes=encoder.initialize().codepage('cp852').align('center').line('ABC')
    .align('left').table([{width:10},{width:6,align:'right'}],[['ABCDEFGHIJK','12,00']]).encode();
  const result=new EscPosPreview(profiles.narrow,18).read(bytes);
  assert.equal(result.block.lines[0].runs[0].x,168);
  assert.deepEqual(result.block.lines.slice(1).map(line=>line.runs.map(run=>run.text.trim()).join('|')),['ABCDEFGHIJ 12,00','K']);
  assert.deepEqual(Uint8Array.from(result.commands.flatMap(c=>c.bytes)),bytes);
});
test('preview fails on unknown/truncated commands instead of silently drawing the wrong receipt', () => {
  for (const bytes of [[27],[27,51],[27,36,0,0],[29,118,48],[1]]) {
    assert.throws(()=>new EscPosPreview(profiles.wide,18).read(Uint8Array.from(bytes)));
  }
});
test('preview records underline spaces and maximum glyph height before a line reset', () => {
  const bytes=Uint8Array.from([27,116,18,27,45,1,29,33,17,32,65,29,33,0,10]);
  const block=new EscPosPreview(profiles.wide,18).read(bytes).block;
  assert.equal(block.lines[0].runs[0].text,' A');
  assert.equal(block.lines[0].runs[0].width,48);
  assert.equal(block.lines[0].height,48);
});
test('concatenated block plans match an independent whole-receipt decode', () => {
  for (const profile of ['wide','narrow']) {
    const t=initialTemplate();t.profile=profile;
    const r=compile(t,sampleData);
    const whole=new EscPosPreview(profiles[profile],18).read(r.bytes).block;
    assert.equal(whole.height,r.plan.height);
    const visible=block=>block.lines.filter(line=>line.runs.length);
    assert.deepEqual(visible(whole),r.plan.blocks.flatMap(visible));
  }
});
