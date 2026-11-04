import {test} from 'node:test';
import assert from 'node:assert/strict';
import {taipeiAccountingDate} from '../../web/src/accounting-date';
test('accounting date uses Taipei at the UTC day boundary',()=>{
 assert.equal(taipeiAccountingDate(new Date('2026-11-03T17:00:00Z')),'2026-11-04');
 assert.equal(taipeiAccountingDate(new Date('2026-12-31T16:00:00Z')),'2027-01-01');
 assert.equal(taipeiAccountingDate(new Date('2026-11-03T15:59:59Z')),'2026-11-03');
});