import test from 'node:test';
import assert from 'node:assert/strict';
import {compile,initialTemplate,sampleData,profiles,validateTemplate} from '../src/core.js';
import {EscPosPreview} from '../src/escpos-preview.js';
import {ReceiptPrinterEncoder} from '../src/rpe-runtime.js';

for(const language of ['star-prnt','star-line']) {
  test(language+' generates native Star commands and preview for both widths',()=>{
    for(const profile of ['wide','narrow']) {
      const t={...initialTemplate(),language,profile,codePage:5,cut:true};
      t.elements.find(e=>e.id==='brand').underline=true;
      const data=structuredClone(sampleData);
      data.items.push({name:'Dugačak artikal sa čćžšđ '.repeat(6),qty:'24',price:'125,00'});
      const r=compile(t,data);
      assert.equal(r.plan.language,language);
      assert.ok(r.commands.some(c=>c.bytes.join()==='27,29,116,5'));
      assert.ok(r.commands.some(c=>c.bytes.join()==='27,105,1,1'));
      assert.ok(r.commands.some(c=>c.bytes.join()==='27,100,1'));
      assert.ok(r.commands.some(c=>c.bytes.join()==='27,73,12'));
      assert.ok(!r.commands.some(c=>c.bytes[0]===29)); // no Epson GS commands
      assert.ok(!r.commands.some(c=>c.bytes[0]===27&&[51,77,116,83].includes(c.bytes[1])));
      const whole=new EscPosPreview(profiles[profile],5,language).read(r.bytes).block;
      assert.equal(whole.height,r.plan.height);
      assert.deepEqual(whole.lines.filter(l=>l.runs.length),r.plan.blocks.flatMap(b=>b.lines.filter(l=>l.runs.length)));
      const table=r.plan.blocks.find(b=>b.id==='items'),total=r.plan.blocks.find(b=>b.id==='total');
      assert.equal(total.y,table.y+table.height);
      assert.equal(r.plan.tailHeight,96);
      const roundtrip=JSON.parse(JSON.stringify(t));
      assert.deepEqual(compile(roundtrip,data).bytes,r.bytes);
    }
  });
}
test('Star parser decodes direct upstream size/bold/codepage/cut commands',()=>{
  const e=new ReceiptPrinterEncoder({language:'star-line',columns:32,newline:'\n',codepageMapping:{cp852:5},feedBeforeCut:0});
  const bytes=e.initialize().font('A').raw([27,122,1]).codepage('cp852')
    .bold(true).underline(true).width(2).height(2).line('ČĆ').cut('partial').encode();
  const r=new EscPosPreview(profiles.narrow,5,'star-line').read(bytes);
  const text=r.block.lines[0].runs.find(r=>r.text==='ČĆ');
  assert.equal(text.width,48);assert.equal(text.charHeight,48);assert.equal(text.bold,true);assert.equal(text.underline,true);
  assert.ok(r.commands.some(c=>c.name.startsWith('ESC d · Star rezanje')));
});
test('protocol changes command semantics: Star ESC d cuts, Epson ESC d feeds',()=>{
  const star=new EscPosPreview(profiles.wide,5,'star-prnt');
  star.read(Uint8Array.from([27,100,1]));
  assert.equal(star.cutY,0);assert.equal(star.y,0);
  const epson=new EscPosPreview(profiles.wide,18);
  epson.read(Uint8Array.from([27,100,1]));
  assert.equal(epson.cutY,null);assert.equal(epson.y,30);
});
test('default protocol is ESC/POS; unsupported protocols and Star emulator export fail',()=>{
  const template=initialTemplate();
  assert.equal(compile(template,sampleData).plan.language,'esc-pos');
  template.language='unknown';assert.throws(()=>validateTemplate(template),/protokol/);
  template.language='star-prnt';template.outputTarget='virtual-printer';
  assert.throws(()=>compile(template,sampleData),/samo ESC\/POS/);
});
test('Star zero gap does not emit an invalid zero paper feed',()=>{
  const t={...initialTemplate(),language:'star-line',codePage:5};
  t.elements.forEach(e=>e.gap=0);
  assert.ok(!compile(t,sampleData).commands.some(c=>c.bytes.join()==='27,73,0'));
});
