// Test data only; does not build application binaries or distribution bundles.
import { readFile, writeFile } from 'node:fs/promises';
import { compile } from '../src/core.js';
const file = new URL('../testdata/encoder.json', import.meta.url);
const cases = JSON.parse(await readFile(file, 'utf8')).filter(fixture => !fixture.name.includes('-star-'));
for (const base of [...cases]) {
  for (const language of ['star-prnt','star-line']) {
    const fixture = structuredClone(base);
    fixture.name += '-' + language;
    fixture.template.language = language;
    fixture.template.codePage = 5;
    fixture.template.cut = true;
    cases.push(fixture);
  }
}
for (const fixture of cases) {
  const result = compile(fixture.template, fixture.data);
  fixture.engine = result.plan.engine;
  fixture.hex = Buffer.from(result.bytes).toString('hex');
  fixture.plan = result.plan;
}
await writeFile(file, JSON.stringify(cases, null, 2) + '\n');
