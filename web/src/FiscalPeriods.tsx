import {useEffect, useState} from 'react';

type Period = {id: string; periodCode: string; startDate: string; endDate: string; isClosed: boolean};
type Request = (path: string, init?: RequestInit) => Promise<unknown>;
const endpoint = '/api/stations/erp/fiscal-periods';

export function FiscalPeriods({request, canClose}: {request: Request; canClose: boolean}) {
  const [periods, setPeriods] = useState<Period[]>();
  const [busy, setBusy] = useState(false);
  const [selected, setSelected] = useState<Period>();
  const [error, setError] = useState('');
  const zh = document.documentElement.lang === 'zh-TW';
  const label = (en: string, tw: string) => zh ? tw : en;

  useEffect(() => {
    const controller = new AbortController();
    request(endpoint, {signal: controller.signal})
      .then(data => { if (!controller.signal.aborted) setPeriods(data as Period[]); })
      .catch(err => { if (!controller.signal.aborted) setError(err.message); });
    return () => controller.abort();
  }, [request]);

  async function close() {
    if (!selected || busy) return;
    setBusy(true);
    setError('');
    try {
      await request(endpoint + '/' + encodeURIComponent(selected.id) + '/close', {method: 'POST'});
      setPeriods(current => current?.map(period => period.id === selected.id ? {...period, isClosed: true} : period));
      setSelected(undefined);
    } catch (err) {
      setError(err instanceof Error ? err.message : label('Unable to close period', '無法關帳'));
    } finally {
      setBusy(false);
    }
  }

  return <section className="panel no-print">
    <h2>{label('Fiscal periods', '會計期間')}</h2>
    {!periods && !error && <p role="status">{label('Loading periods…', '載入會計期間…')}</p>}
    {periods?.length === 0 && <p>{label('No fiscal periods', '沒有會計期間')}</p>}
    {periods && periods.length > 0 && <ul>{periods.map(period => <li key={period.id}>
      <strong>{period.periodCode}</strong> · {period.startDate} – {period.endDate} · {period.isClosed ? label('Closed', '已關帳') : label('Open', '未關帳')}
      {canClose && !period.isClosed && <button type="button" className="secondary" disabled={busy} onClick={() => setSelected(period)}>{label('Close period', '關帳')}</button>}
    </li>)}</ul>}
    {selected && <div>
      <p>{label('Close period', '關帳')} {selected.periodCode}? {label('Journal posting will be disabled for these dates.', '該期間將停止接受傳票過帳。')}</p>
      <button type="button" disabled={busy} onClick={() => void close()}>{label('Confirm close', '確認關帳')}</button>
      <button type="button" className="secondary" disabled={busy} onClick={() => setSelected(undefined)}>{label('Cancel', '取消')}</button>
    </div>}
    {error && <p role="alert" className="error">{error}</p>}
  </section>;
}
