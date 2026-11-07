import React, { useEffect, useState } from 'react';
import { tr } from './locale';

type Row = Record<string, any>;
type ApiResult<T> = {
  success: boolean;
  data: T;
  error?: { message?: string };
};

type TransferIntent = {
  itemId: string;
  fromWarehouseId: string;
  toWarehouseId: string;
  quantity: number;
  requestId: string;
};

class DefinitiveTransferError extends Error {}

type StockTransferProps = {
  data: Record<string, Row[]>;
  role: string;
  onChanged: () => Promise<void>;
};

export function StockTransfer({ data, role, onChanged }: StockTransferProps) {
  const [item, setItem] = useState('');
  const [from, setFrom] = useState('');
  const [to, setTo] = useState('');
  const [quantity, setQuantity] = useState('1');
  const [available, setAvailable] = useState<number | null>(null);
  const [availabilityRefresh, setAvailabilityRefresh] = useState(0);
  const [intent, setIntent] = useState<TransferIntent | null>(null);
  const [intentLocked, setIntentLocked] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [message, setMessage] = useState('');

  const items = (data.items || []).filter(row => row.isActive);
  const warehouses = data.warehouses || [];
  const locked = Boolean(intent) && intentLocked;

  useEffect(() => {
    if (!item && items[0]) setItem(items[0].id);
    if (!from && warehouses[0]) setFrom(warehouses[0].id);
    if (!to && warehouses[1]) setTo(warehouses[1].id);
  }, [data]);

  useEffect(() => {
    setAvailable(null);
    setError('');
    if (!item || !from) return;

    const controller = new AbortController();
    const params = new URLSearchParams({ itemId: item, warehouseId: from });
    fetch(`/api/stations/erp/inventory?${params}`, {
      credentials: 'same-origin',
      signal: controller.signal,
    })
      .then(async response => {
        const result: ApiResult<{ available: number }> = await response.json();
        if (!response.ok || !result.success) {
          throw new Error(result.error?.message || 'Availability lookup failed');
        }
        setAvailable(result.data.available);
      })
      .catch(cause => {
        if (cause.name !== 'AbortError') setError(cause.message);
      });

    return () => controller.abort();
  }, [data, item, from, availabilityRefresh]);

  async function sendTransfer() {
    setBusy(true);
    setError('');
    setMessage('');

    const currentIntent = locked && intent
      ? intent
      : {
          itemId: item,
          fromWarehouseId: from,
          toWarehouseId: to,
          quantity: Number(quantity),
          requestId: crypto.randomUUID(),
        };
    setIntent(currentIntent);
    setIntentLocked(true);

    try {
      const response = await fetch('/api/stations/erp/inventory', {
        method: 'POST',
        credentials: 'same-origin',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ action: 'transfer', ...currentIntent }),
      });
      const result: ApiResult<{ sourceBalanceAfter: number; destBalanceAfter: number }> = await response.json();
      if (!response.ok || !result.success) {
        throw new DefinitiveTransferError(result.error?.message || 'Transfer failed');
      }

      setMessage(
        `${tr('Transfer complete')} · ${tr('Source available')}: ${result.data.sourceBalanceAfter} · ` +
        `${tr('Destination balance')}: ${result.data.destBalanceAfter}`,
      );
      setIntent(null);
      setIntentLocked(false);
      setAvailable(null);
      setAvailabilityRefresh(value => value + 1);
      await onChanged();
    } catch (cause) {
      setError((cause as Error).message);
      if (cause instanceof DefinitiveTransferError) setIntentLocked(false);
      // Keep an immutable payload and key after transport failures for safe replay.
    } finally {
      setBusy(false);
    }
  }

  async function submit(event: React.FormEvent) {
    event.preventDefault();
    await sendTransfer();
  }

  function discardRejectedIntent() {
    setIntent(null);
    setIntentLocked(false);
    setError('');
    setMessage('');
  }

  if (role === 'readonly') return null;
  if (!items.length || warehouses.length < 2) {
    return (
      <section className="panel no-print">
        <h2>{tr('Warehouse transfer')}</h2>
        <p>{tr('Loading inventory…')}</p>
      </section>
    );
  }

  const shownItem = locked ? intent!.itemId : item;
  const shownFrom = locked ? intent!.fromWarehouseId : from;
  const shownTo = locked ? intent!.toWarehouseId : to;
  const shownQuantity = locked ? String(intent!.quantity) : quantity;

  return (
    <section className="panel no-print" aria-label={tr('Warehouse transfer')}>
      <h2>{tr('Warehouse transfer')}</h2>
      <p>{tr('Availability is read from the persisted ledger. Retry the same request after an uncertain result.')}</p>
      <form onSubmit={submit}>
        <fieldset disabled={busy || locked}>
          <label>
            {tr('Transfer item')}
            <select
              aria-label={tr('Transfer item')}
              required
              value={shownItem}
              onChange={event => setItem(event.target.value)}
            >
              {items.map(row => <option key={row.id} value={row.id}>{row.name} ({row.sku})</option>)}
            </select>
          </label>
          <label>
            {tr('Source warehouse')}
            <select required value={shownFrom} onChange={event => setFrom(event.target.value)}>
              {warehouses.map(row => <option key={row.id} value={row.id}>{row.name}</option>)}
            </select>
          </label>
          <label>
            {tr('Destination warehouse')}
            <select required value={shownTo} onChange={event => setTo(event.target.value)}>
              {warehouses.map(row => <option key={row.id} value={row.id}>{row.name}</option>)}
            </select>
          </label>
          <p>{tr('Available')}: {available === null ? tr('Loading…') : available}</p>
          <label>
            {tr('Transfer quantity')}
            <input
              type="number"
              min="1"
              max={available ?? undefined}
              step="1"
              required
              value={shownQuantity}
              onChange={event => setQuantity(event.target.value)}
            />
          </label>
          {!intent && (
            <button
              disabled={busy || from === to || available === null || Number(quantity) < 1 || !Number.isSafeInteger(Number(quantity))}
            >
              {busy ? tr('Transferring…') : tr('Confirm stock transfer')}
            </button>
          )}
        </fieldset>
      </form>

      {intent && (
        <div>
          <p>{tr('Retry key retained')}: <code>{intent.requestId}</code></p>
          {intentLocked
            ? <button type="button" className="secondary" disabled={busy} onClick={() => void sendTransfer()}>{tr('Retry transfer')}</button>
            : <p>{tr('The prior response definitively rejected this payload.')}</p>}
          <button type="button" className="secondary" disabled={busy} onClick={discardRejectedIntent}>
            {tr('Discard rejected transfer')}
          </button>
        </div>
      )}
      {error && <p role="alert" className="error">{error}</p>}
      {message && <p role="status">{message}</p>}
    </section>
  );
}
