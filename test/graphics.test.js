import test from 'node:test';
import assert from 'node:assert/strict';
import jsQR from 'jsqr';
import { compile, initialTemplate, newElement, profiles, validateTemplate } from '../src/core.js';
import { EscPosPreview } from '../src/escpos-preview.js';

function receipt(language, elements, profile = 'wide', data = {}) {
  const template = { ...initialTemplate(), elements, profile, language, codePage: language === 'esc-pos' ? 18 : 5 };
  return { template, ...compile(template, data) };
}
function pixel(graphics, x, y) {
  const g = graphics.find(g => x >= g.x && x < g.x + g.width && y >= g.y && y < g.y + g.height);
  if (!g) return 0;
  const gx = x - g.x, gy = y - g.y;
  return g.data[gy * Math.ceil(g.width / 8) + (gx >> 3)] & (128 >> (gx & 7)) ? 1 : 0;
}
function decode(block, width) {
  const rgba = new Uint8ClampedArray(width * block.height * 4).fill(255);
  for (let y = 0; y < block.height; y++) for (let x = 0; x < width; x++) if (pixel(block.graphics, x, y + block.y)) {
    const i = (y * width + x) * 4; rgba[i] = rgba[i + 1] = rgba[i + 2] = 0;
  }
  return jsQR(rgba, width, block.height)?.data;
}
for (const language of ['esc-pos', 'star-prnt', 'star-line']) {
  test(`${language}: QR commands contain bound data; native preview and raster Unicode QR are scannable`, () => {
    for (const qrMode of ['native', 'image']) for (const profile of ['wide', 'narrow']) {
      const content = qrMode === 'native' ? 'https://example.com/receipt/42' : 'Čćžšđ ✓ račun 42';
      const qr = {...newElement('qr','qr'), content:'{{url}}', qrMode, moduleSize:3};
      const after = {...newElement('text','after'), text:'UKUPNO: 42,00 KM čćžšđ', bold:true};
      const r = receipt(language, [qr, after], profile, {url:content});
      assert.equal(decode(r.plan.blocks[0], profiles[profile].width), content);
      const commands = r.commands.filter(c => c.name.includes('QR'));
      assert.equal(commands.length > 0, qrMode === 'native');
      assert.equal(r.plan.blocks[1].y, r.plan.blocks[0].height);
      assert.equal(r.plan.blocks[1].lines[0].runs[0].text, after.text);
      assert.equal(r.plan.blocks[1].lines[0].runs[0].bold, true);
      const whole = new EscPosPreview(profiles[profile], r.template.codePage, language).read(r.bytes).block;
      assert.deepEqual(whole.graphics, r.plan.blocks.flatMap(b => b.graphics || []));
      assert.equal(whole.height, r.plan.height);
      assert.deepEqual(compile(JSON.parse(JSON.stringify(r.template)), {url:content}).bytes, r.bytes);
    }
  });
  test(`${language}: logo orientation, white padding, threshold and subsequent text remain intact`, () => {
    for (const align of ['left','center','right']) {
      const logo = {...newElement('image','logo'), image:{width:3,height:2,gray:'00ffffffffff'},imageWidth:24,x:25,width:50,align};
      const after = {...newElement('text','after'),text:'After'};
      const r=receipt(language,[logo,after]);
      const block=r.plan.blocks[0];
      const left=144+(align==='right'?264:align==='center'?132:0);
      assert.equal(pixel(block.graphics,left,block.y),1);
      assert.equal(pixel(block.graphics,left+8,block.y),0);
      assert.equal(pixel(block.graphics,left,block.y+8),0);
      assert.equal(pixel(block.graphics,left-1,block.y),0);
      assert.equal(r.plan.blocks[1].y,block.height);
      assert.ok(r.commands.some(c=>c.name.startsWith('TEXT') && c.name.includes('After')));
    }
  });
}
test('bad graphics and unsupported native QR content are rejected before export',()=>{
  const qr={...newElement('qr','qr'),content:'{{missing}}'};
  assert.throws(()=>receipt('esc-pos',[qr]),/Nedostaje podatak/);
  qr.content='čć'; assert.throws(()=>receipt('esc-pos',[qr]),/ASCII/);
  qr.content=''; assert.throws(()=>receipt('esc-pos',[qr]),/1–1000/);
  qr.content='x'.repeat(1001); assert.throws(()=>receipt('esc-pos',[qr]),/1–1000/);
  qr.content='x'.repeat(250);qr.moduleSize=8;assert.throws(()=>receipt('esc-pos',[qr],'narrow'),/preširok/);
  const image=newElement('image','image');assert.throws(()=>receipt('esc-pos',[image]),/Odaberi/);
  image.image={width:2,height:2,gray:'00'};assert.throws(()=>receipt('esc-pos',[image]),/pikseli/);
  image.image={width:1,height:256,gray:'00'.repeat(256)};assert.throws(()=>receipt('esc-pos',[image]),/previsoka/);
  image.image={width:257,height:1,gray:'00'.repeat(257)};assert.throws(()=>receipt('esc-pos',[image]),/pikseli/);
  assert.throws(()=>validateTemplate({...initialTemplate(),elements:[{...newElement('qr','q'),moduleSize:1.2}]}),/svojstva QR/);
});
test('image height above 255 uses multiple contiguous raster chunks without losing rows',()=>{
  const image={...newElement('image','logo'),image:{width:1,height:2,gray:'00ff'},imageWidth:144};
  const r=receipt('esc-pos',[image]);
  assert.equal(r.plan.blocks[0].graphics.length,2);
  const [a,b]=r.plan.blocks[0].graphics;
  assert.equal(a.height,255);assert.equal(b.y,a.y+a.height);
  assert.equal(a.height+b.height,288);
});
test('truncated graphic payloads fail, rather than treating their bytes as text',()=>{
  for(const language of ['esc-pos','star-line']) {
    const image={...newElement('image','logo'),image:{width:1,height:1,gray:'00'},imageWidth:24};
    const r=receipt(language,[image]);
    const cmd=r.commands.find(c=>c.name.includes('raster slike')||c.name.includes('Star slika'));
    assert.throws(()=>new EscPosPreview(profiles.wide,r.template.codePage,language).read(Uint8Array.from(cmd.bytes.slice(0,-1))),/Nepotpuna/);
  }
});

test('packed graphic adapter preserves RPE image command bytes without redundant RGBA processing', async()=>{
  const {ReceiptPrinterEncoder}=await import('../src/rpe-runtime.js');
  const {printGraphic}=await import('../src/graphics.js');
  for(const language of ['esc-pos','star-line']) {
    const width=32,height=32,data=new Uint8Array(width*height/8);
    data[0]=0x81;data[9]=0xa5;data[127]=0xff;
    const rgba=new Uint8ClampedArray(width*height*4).fill(255);
    for(let y=0;y<height;y++)for(let x=0;x<width;x++)if(data[y*4+(x>>3)]&(128>>(x&7))){const i=(y*width+x)*4;rgba[i]=rgba[i+1]=rgba[i+2]=0;}
    const options={language,columns:48,newline:'\n'};
    const upstream=new ReceiptPrinterEncoder(options).image({width,height,data:rgba},{width,height,mode:'raster'}).encode();
    const adapter=new ReceiptPrinterEncoder(options);printGraphic(adapter,{width,height,data},language!=='esc-pos');
    assert.deepEqual(adapter.encode(),upstream);
  }
});
