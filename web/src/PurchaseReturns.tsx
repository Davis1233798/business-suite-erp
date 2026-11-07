import React, { useEffect, useState } from 'react';
import { tr } from './locale';

type Row = Record<string, any>;

type ReturnAvailability = {
  receiptLineId: string;
  itemId: string;
  warehouseId: string;
  receivedQuantity: number;
  returnedQuantity: number;
  remainingQuantity: number;
  warehouseStock: number;
  available: number;
};

type PurchaseReturnsProps = {
  data: Record<string, Row[]>;
  role: string;
  onChanged: () => Promise<void>;
};

export function PurchaseReturns({ data, role, onChanged }: PurchaseReturnsProps) {
  const [selectedLine, setSelectedLine] = useState('');
  const [quantity, setQuantity] = useState('1');
  const [reason, setReason] = useState('Supplier inspection failed');
  const [availability, setAvailability] = useState<ReturnAvailability | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [message, setMessage] = useState('');

  const receiptLines = data.receiptLines || [];
  const receipts = data.receipts || [];
  const items = data.items || [];
  const warehouses = data.warehouses || [];

  const receiptMap = new Map(receipts.map(r => [r.id, r]));
  const itemMap = new Map(items.map(i => [i.id, i.name || i.sku || i.id]));
  const warehouseMap = new Map(warehouses.map(w => [w.id, w.name || w.code || w.id]));

  useEffect(() => {
    if (!selectedLine && receiptLines.length > 0) {
      setSelectedLine(receiptLines[0].id);
    }
  }, [receiptLines, selectedLine]);

  useEffect(() => {
    if (!selectedLine) {
      setAvailability(null);
      return;
    }
    let active = true;
    fetch(`/api/stations/erp/purchase-returns?receiptLineId=${encodeURIComponent(selectedLine)}`, {
      credentials: 'same-origin',
    })
      .then(async res => {
        const json = await res.json();
        if (active) {
          if (res.ok && json.success) {
            setAvailability(json.data);
            setError('');
          } else {
            setAvailability(null);
            setError(json.error?.message || tr('Failed to read return availability'));
          }
        }
      })
      .catch(err => {
        if (active) {
          setAvailability(null);
          setError(err instanceof Error ? err.message : tr('Failed to read return availability'));
        }
      });
    return () => {
      active = false;
    };
  }, [selectedLine]);

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (!selectedLine || busy) return;
    const qty = parseInt(quantity, 10);
    if (isNaN(qty) || qty < 1) {
      setError(tr('Provide a positive integer quantity'));
      return;
    }
    if (!reason.trim()) {
      setError(tr('Provide a valid return reason'));
      return;
    }
    setBusy(true);
    setError('');
    setMessage('');
    try {
      const res = await fetch('/api/stations/erp/purchase-returns', {
        method: 'POST',
        credentials: 'same-origin',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          receiptLineId: selectedLine,
          quantity: qty,
          reason: reason.trim(),
          requestId: crypto.randomUUID(),
        }),
      });
      const json = await res.json();
      if (!res.ok || !json.success) {
        throw new Error(json.error?.message || tr('Purchase return failed'));
      }
      setMessage(`${tr('Purchase return saved')}: ${json.data.id}`);
      await onChanged();
      // Refresh availability
      const availRes = await fetch(`/api/stations/erp/purchase-returns?receiptLineId=${encodeURIComponent(selectedLine)}`, {
        credentials: 'same-origin',
      });
      const availJson = await availRes.json();
      if (availRes.ok && availJson.success) {
        setAvailability(availJson.data);
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : tr('Purchase return failed'));
    } finally {
      setBusy(false);
    }
  }

  const canReturn = role === 'admin' || role === 'operator';

  return (
    <section className="panel no-print" aria-labelledby="purchase-returns-heading">
      <h2 id="purchase-returns-heading">{tr('Supplier stock returns')}</h2>
      <p>{tr('Return goods from a completed receipt back to the supplier.')}</p>

      {receiptLines.length === 0 ? (
        <p className="empty">{tr('No receipt records available for return.')}</p>
      ) : (
        <form onSubmit={handleSubmit} className="form-grid">
          <label>
            {tr('Receipt line')}
            <select
              aria-label={tr('Receipt line')}
              value={selectedLine}
              onChange={e => {
                setSelectedLine(e.target.value);
                setMessage('');
                setError('');
              }}
              disabled={busy}
            >
              {receiptLines.map(line => {
                const receipt = receiptMap.get(line.receiptId);
                const itemName = itemMap.get(line.itemId) || line.itemId;
                const whName = receipt ? warehouseMap.get(receipt.warehouseId) || receipt.warehouseId : '';
                return (
                  <option key={line.id} value={line.id}>
                    {receipt ? receipt.receiptNumber : line.receiptId} · {itemName} ({line.quantity}) · {whName}
                  </option>
                );
              })}
            </select>
          </label>

          {availability && (
            <div className="metrics" style={{ gridColumn: '1 / -1' }}>
              <div>
                <small>{tr('Received quantity')}</small>
                <strong>{availability.receivedQuantity}</strong>
              </div>
              <div>
                <small>{tr('Returned quantity')}</small>
                <strong>{availability.returnedQuantity}</strong>
              </div>
              <div>
                <small>{tr('Remaining returnable')}</small>
                <strong>{availability.remainingQuantity}</strong>
              </div>
              <div>
                <small>{tr('Warehouse stock')}</small>
                <strong>{availability.warehouseStock}</strong>
              </div>
              <div>
                <small>{tr('Max available to return')}</small>
                <strong>{availability.available}</strong>
              </div>
            </div>
          )}

          <label>
            {tr('Return quantity')}
            <input
              type="number"
              aria-label={tr('Return quantity')}
              min="1"
              max={availability ? availability.available : 10000000}
              value={quantity}
              onChange={e => setQuantity(e.target.value)}
              required
              disabled={busy || !canReturn}
            />
          </label>

          <label>
            {tr('Return reason')}
            <input
              type="text"
              aria-label={tr('Return reason')}
              maxLength={500}
              value={reason}
              onChange={e => setReason(e.target.value)}
              required
              disabled={busy || !canReturn}
            />
          </label>

          <div style={{ gridColumn: '1 / -1', marginTop: '0.5rem' }}>
            <button
              type="submit"
              disabled={busy || !canReturn || !availability || availability.available <= 0}
            >
              {busy ? tr('Processing return…') : tr('Submit return')}
            </button>
          </div>
        </form>
      )}

      {error && <p role="alert" className="error">{error}</p>}
      {message && <p role="status" className="success">{message}</p>}
    </section>
  );
}
