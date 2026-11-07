import { useEffect, useState, type FormEvent } from 'react';
import { z } from 'zod';
import { tr } from './locale';

const endpoint = '/api/stations/erp/stock-count';
const snapshotResponse = z.object({
  success: z.literal(true),
  data: z.object({ quantity: z.number().int().nonnegative(), version: z.string() }),
});
const countResponse = z.object({
  success: z.literal(true),
  data: z.object({ referenceDoc: z.string(), balanceAfter: z.number().int().nonnegative() }),
});
const stockOption = z.object({ id: z.string(), name: z.string(), isActive: z.union([z.boolean(), z.number()]) });
type StockData = Record<string, unknown[]>;
type Snapshot = { itemId: string; warehouseId: string; quantity: number; version: string };
type Command = {
  itemId: string;
  warehouseId: string;
  expectedQuantity: number;
  expectedVersion: string;
  countedQuantity: number;
  reason: string;
  idempotencyKey: string;
};

export function StockCount({ data, onChanged }: { data: StockData; onChanged: () => Promise<void> }) {
  const [item, setItem] = useState('');
  const [warehouse, setWarehouse] = useState('');
  const [counted, setCounted] = useState('');
  const [reason, setReason] = useState('');
  const [snapshot, setSnapshot] = useState<Snapshot | null>(null);
  const [refresh, setRefresh] = useState(0);
  const [pending, setPending] = useState<Command | null>(null);
  const [busy, setBusy] = useState(false);
  const [lookupError, setLookupError] = useState(false);
  const [error, setError] = useState('');
  const [reference, setReference] = useState('');
  const parsedItems = stockOption.array().safeParse(data.items);
  const parsedWarehouses = stockOption.array().safeParse(data.warehouses);
  const items = parsedItems.success ? parsedItems.data.filter(row => row.isActive) : [];
  const warehouses = parsedWarehouses.success ? parsedWarehouses.data.filter(row => row.isActive) : [];
  const itemId = items.find(row => row.id === item)?.id ?? items[0]?.id ?? '';
  const warehouseId = warehouses.find(row => row.id === warehouse)?.id ?? warehouses[0]?.id ?? '';
  const current = snapshot?.itemId === itemId && snapshot.warehouseId === warehouseId ? snapshot : null;
  const quantity = Number(counted);
  const canSubmit = current && counted !== '' && Number.isSafeInteger(quantity) &&
    quantity >= 0 && quantity !== current.quantity && reason.trim() !== '';

  useEffect(() => {
    setSnapshot(null);
    setLookupError(false);
    if (!itemId || !warehouseId) return;
    const abort = new AbortController();
    const query = new URLSearchParams({ itemId, warehouseId });
    async function loadSnapshot() {
      try {
        const response = await fetch(`${endpoint}?${query}`, { signal: abort.signal });
        if (!response.ok) throw new Error('Inventory lookup failed');
        const result = snapshotResponse.parse(await response.json());
        if (!abort.signal.aborted) setSnapshot({ itemId, warehouseId, ...result.data });
      } catch {
        if (!abort.signal.aborted) setLookupError(true);
      }
    }
    void loadSnapshot();
    return () => abort.abort();
  }, [data, itemId, warehouseId, refresh]);

  async function submit(event: FormEvent) {
    event.preventDefault();
    if (busy) return;
    let command = pending;
    if (!command) {
      if (!current || !canSubmit) return;
      command = {
        itemId,
        warehouseId,
        expectedQuantity: current.quantity,
        expectedVersion: current.version,
        countedQuantity: quantity,
        reason: reason.trim(),
        idempotencyKey: crypto.randomUUID(),
      };
    }
    setPending(command);
    setBusy(true);
    setError('');
    setReference('');
    try {
      const response = await fetch(endpoint, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(command),
      });
      if (response.status >= 400 && response.status < 500) {
        setPending(null);
        setError(tr(response.status === 409 ? 'Review the current stock before submitting again.' : 'The stock count was rejected.'));
        setRefresh(value => value + 1);
        return;
      }
      if (!response.ok) throw new Error('Uncertain count result');
      const result = countResponse.parse(await response.json());
      setPending(null);
      setReference(result.data.referenceDoc);
      setCounted('');
      setReason('');
      setRefresh(value => value + 1);
      try {
        await onChanged();
      } catch {
        setError(tr('The count was saved. Reload the workspace to refresh the ledger.'));
      }
    } catch {
      setError(tr('The result is uncertain. Retry the original count.'));
    } finally {
      setBusy(false);
    }
  }

  return <section className="panel no-print" aria-label={tr('Physical stock count')}>
    <h2>{tr('Physical stock count')}</h2>
    <p>{tr('Record the observed stock and explain any difference.')}</p>
    <form onSubmit={submit}>
      <fieldset disabled={busy || pending !== null}>
        <legend>{tr('Count details')}</legend>
        <label>{tr('Count item')}
          <select aria-label={tr('Count item')} required value={itemId} onChange={event => setItem(event.target.value)}>
            {items.map(row => <option key={row.id} value={row.id}>{row.name}</option>)}
          </select>
        </label>
        <label>{tr('Count warehouse')}
          <select aria-label={tr('Count warehouse')} required value={warehouseId} onChange={event => setWarehouse(event.target.value)}>
            {warehouses.map(row => <option key={row.id} value={row.id}>{row.name}</option>)}
          </select>
        </label>
        <p>{current
          ? `${tr('Current stock: ')}${current.quantity}`
          : tr(lookupError ? 'Current stock could not be loaded.' : 'Waiting for the current stock…')}</p>
        <button type="button" className="secondary" onClick={() => setRefresh(value => value + 1)}>
          {tr('Refresh current stock')}
        </button>
        <label>{tr('Counted quantity')}
          <input type="number" min="0" max={Number.MAX_SAFE_INTEGER} step="1" required
            value={counted} onChange={event => setCounted(event.target.value)}/>
        </label>
        <label>{tr('Count reason')}
          <input required maxLength={500} value={reason} onChange={event => setReason(event.target.value)}/>
        </label>
      </fieldset>
      <button type="submit" disabled={busy || (!pending && !canSubmit)}>
        {tr(pending && !busy ? 'Retry original count' : 'Approve stock count')}
      </button>
    </form>
    {error && <p role="alert" className="error">{error}</p>}
    {reference && <p role="status">{tr('Stock count saved')} · {reference}</p>}
  </section>;
}
