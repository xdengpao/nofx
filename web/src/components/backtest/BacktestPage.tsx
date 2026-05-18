import { useMemo, useState } from 'react';
import useSWR from 'swr';
import { StrategyCandlestickChart } from '../StrategyCandlestickChart';
import { backtestApi } from '../../lib/backtestApi';

const panelStyle = { background: '#181A20', border: '1px solid #2B3139' };

export function BacktestPage() {
  const [symbols, setSymbols] = useState('BTCUSDT,ETHUSDT');
  const [dataFrom, setDataFrom] = useState('2026-01-01');
  const [dataTo, setDataTo] = useState('2026-02-01');
  const [backtestFrom, setBacktestFrom] = useState('2026-01-10');
  const [backtestTo, setBacktestTo] = useState('2026-02-01');
  const [tradeLevel, setTradeLevel] = useState('1h');
  const [initialEquity, setInitialEquity] = useState(10000);
  const [runId, setRunId] = useState('');
  const [selectedSymbol, setSelectedSymbol] = useState('BTCUSDT');
  const [message, setMessage] = useState('');

  const { data: coverage, mutate: refreshCoverage } = useSWR('backtest-coverage', backtestApi.inspect, {
    refreshInterval: 30000,
  });
  const { data: jobs, mutate: refreshJobs } = useSWR('backtest-jobs', backtestApi.jobs, {
    refreshInterval: 3000,
  });
  const { data: report } = useSWR(runId ? `backtest-report-${runId}` : null, () => backtestApi.report(runId));
  const { data: klines } = useSWR(
    selectedSymbol ? `backtest-klines-${selectedSymbol}-${tradeLevel}-${backtestFrom}-${backtestTo}` : null,
    () => backtestApi.klines(selectedSymbol, tradeLevel, backtestFrom, backtestTo),
  );
  const { data: markers } = useSWR(
    runId && selectedSymbol ? `backtest-markers-${runId}-${selectedSymbol}-${tradeLevel}` : null,
    () => backtestApi.markers(runId, selectedSymbol, tradeLevel).catch(() => []),
  );

  const symbolList = useMemo(() => splitSymbols(symbols), [symbols]);

  const startFetch = async () => {
    setMessage('');
    const job = await backtestApi.fetchHistory({
      source: 'binance-futures',
      symbols: symbolList,
      timeframes: ['3m', '15m', '1h', '4h'],
      data_from: dataFrom,
      data_to: dataTo,
      timezone: 'Asia/Singapore',
      rate_limit: { requests_per_minute: 120, concurrency: 1, page_limit: 1000 },
    });
    setMessage(`history job ${job.run_id}`);
    refreshJobs();
    refreshCoverage();
  };

  const startRun = async () => {
    setMessage('');
    const job = await backtestApi.run({
      backtest_from: backtestFrom,
      backtest_to: backtestTo,
      timezone: 'Asia/Singapore',
      symbols: symbolList,
      initial_equity: initialEquity,
      scan_interval_minutes: 3,
      strategy: {
        decision_mode: 'programmatic',
        programmatic_strategy: {
          timeframes: { trade: tradeLevel },
          state: { bootstrap: false },
        },
      },
      costs: { taker_fee_bps: 5, maker_fee_bps: 2, slippage_bps: 3 },
      execution: {
        market_order_fill: 'next_3m_open',
        same_bar_conflict: 'worst_case',
        funding_mode: 'disabled',
        liquidation_mode: 'not_modelled',
      },
    });
    setMessage(`run job ${job.run_id}`);
    refreshJobs();
  };

  const latestCompletedRun = jobs?.find((job) => job.progress?.run_id && job.status === 'completed')?.progress?.run_id;

  return (
    <div className="space-y-5">
      <div className="flex flex-col gap-2 sm:flex-row sm:items-end sm:justify-between">
        <div>
          <div className="text-xs font-mono" style={{ color: '#F0B90B' }}>BACKTEST · dry_run=true · live_trading=false</div>
          <h1 className="text-2xl font-bold mt-1">程序化策略回测</h1>
        </div>
        <div className="flex gap-2">
          {latestCompletedRun && (
            <button className="px-3 py-2 rounded text-sm font-semibold" style={buttonStyle(false)} onClick={() => setRunId(latestCompletedRun)}>
              载入最新报告
            </button>
          )}
          <input value={runId} onChange={(event) => setRunId(event.target.value)} placeholder="run_id" className="rounded px-3 py-2 text-sm font-mono" style={inputStyle} />
        </div>
      </div>

      <div className="grid grid-cols-1 xl:grid-cols-[360px_1fr] gap-5">
        <div className="space-y-5">
          <section className="rounded p-4" style={panelStyle}>
            <h2 className="font-bold mb-3">历史数据</h2>
            <Field label="Symbols" value={symbols} onChange={setSymbols} />
            <div className="grid grid-cols-2 gap-3">
              <Field label="data_from" value={dataFrom} onChange={setDataFrom} />
              <Field label="data_to" value={dataTo} onChange={setDataTo} />
            </div>
            <button className="mt-3 w-full px-3 py-2 rounded text-sm font-semibold" style={buttonStyle(true)} onClick={startFetch}>
              获取历史数据
            </button>
            <div className="mt-4 max-h-52 overflow-auto text-xs font-mono space-y-1" style={{ color: '#848E9C' }}>
              {(coverage ?? []).slice(0, 30).map((item) => (
                <div key={`${item.symbol}-${item.timeframe}`}>
                  {item.symbol} {item.timeframe} {item.count} {formatTime(item.from_ms)} → {formatTime(item.to_ms)}
                </div>
              ))}
            </div>
          </section>

          <section className="rounded p-4" style={panelStyle}>
            <h2 className="font-bold mb-3">回测配置</h2>
            <div className="grid grid-cols-2 gap-3">
              <Field label="backtest_from" value={backtestFrom} onChange={setBacktestFrom} />
              <Field label="backtest_to" value={backtestTo} onChange={setBacktestTo} />
            </div>
            <label className="block text-xs mt-3" style={{ color: '#848E9C' }}>
              trade level
              <select value={tradeLevel} onChange={(event) => setTradeLevel(event.target.value)} className="mt-1 w-full rounded px-3 py-2 text-sm" style={inputStyle}>
                <option value="15m">15m</option>
                <option value="1h">1h</option>
                <option value="4h">4h</option>
              </select>
            </label>
            <label className="block text-xs mt-3" style={{ color: '#848E9C' }}>
              initial equity
              <input type="number" value={initialEquity} onChange={(event) => setInitialEquity(Number(event.target.value))} className="mt-1 w-full rounded px-3 py-2 text-sm" style={inputStyle} />
            </label>
            <button className="mt-3 w-full px-3 py-2 rounded text-sm font-semibold" style={buttonStyle(true)} onClick={startRun}>
              启动单次回测
            </button>
            {message && <div className="mt-3 text-xs font-mono" style={{ color: '#F0B90B' }}>{message}</div>}
          </section>
        </div>

        <div className="space-y-5">
          <section className="rounded p-4" style={panelStyle}>
            <div className="flex items-center justify-between gap-3 mb-3">
              <h2 className="font-bold">运行状态</h2>
              <button className="px-3 py-1.5 rounded text-xs font-semibold" style={buttonStyle(false)} onClick={() => refreshJobs()}>刷新</button>
            </div>
            <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-3">
              {(jobs ?? []).slice(-6).reverse().map((job) => (
                <div key={job.run_id} className="rounded p-3 text-xs" style={{ background: '#0B0E11', border: '1px solid #2B3139' }}>
                  <div className="font-mono font-bold">{job.progress?.run_id || job.run_id}</div>
                  <div className="mt-1" style={{ color: statusColor(job.status) }}>{job.status}</div>
                  <div className="mt-2 font-mono" style={{ color: '#848E9C' }}>
                    cycles {job.progress?.cycles ?? 0} · exec {job.progress?.executions ?? 0} · signals {job.progress?.signals ?? 0}
                  </div>
                  {job.progress?.run_id && (
                    <button className="mt-2 px-2 py-1 rounded font-semibold" style={buttonStyle(false)} onClick={() => setRunId(job.progress!.run_id)}>
                      报告
                    </button>
                  )}
                </div>
              ))}
            </div>
          </section>

          {report && (
            <section className="rounded p-4" style={panelStyle}>
              <h2 className="font-bold mb-3">报告总览</h2>
              <div className="grid grid-cols-2 lg:grid-cols-4 gap-3">
                <Metric label="净收益" value={`${report.summary.net_pnl.toFixed(2)} USDT`} tone={report.summary.net_pnl >= 0 ? 'good' : 'bad'} />
                <Metric label="收益率" value={`${report.summary.net_return_pct.toFixed(2)}%`} tone={report.summary.net_return_pct >= 0 ? 'good' : 'bad'} />
                <Metric label="最大回撤" value={`${report.summary.max_drawdown_pct.toFixed(2)}%`} />
                <Metric label="胜率" value={`${report.summary.win_rate.toFixed(1)}%`} />
                <Metric label="PF" value={report.summary.profit_factor.toFixed(2)} />
                <Metric label="交易数" value={String(report.summary.trade_count)} />
                <Metric label="手续费" value={report.summary.total_fees.toFixed(2)} />
                <Metric label="拒绝数" value={String(report.summary.rejection_count)} />
              </div>
            </section>
          )}

          <section className="rounded p-4" style={panelStyle}>
            <div className="flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between mb-3">
              <h2 className="font-bold">K线复盘</h2>
              <select value={selectedSymbol} onChange={(event) => setSelectedSymbol(event.target.value)} className="rounded px-3 py-2 text-sm font-mono" style={inputStyle}>
                {symbolList.map((symbol) => <option key={symbol} value={symbol}>{symbol}</option>)}
              </select>
            </div>
            <StrategyCandlestickChart
              symbol={selectedSymbol}
              timeframe={tradeLevel}
              klines={klines?.klines ?? []}
              markers={markers ?? []}
              limit={klines?.limit}
            />
          </section>
        </div>
      </div>
    </div>
  );
}

function Field({ label, value, onChange }: { label: string; value: string; onChange: (value: string) => void }) {
  return (
    <label className="block text-xs mt-3" style={{ color: '#848E9C' }}>
      {label}
      <input value={value} onChange={(event) => onChange(event.target.value)} className="mt-1 w-full rounded px-3 py-2 text-sm font-mono" style={inputStyle} />
    </label>
  );
}

function Metric({ label, value, tone }: { label: string; value: string; tone?: 'good' | 'bad' }) {
  const color = tone === 'good' ? '#0ECB81' : tone === 'bad' ? '#F6465D' : '#EAECEF';
  return (
    <div className="rounded p-3" style={{ background: '#0B0E11', border: '1px solid #2B3139' }}>
      <div className="text-xs" style={{ color: '#848E9C' }}>{label}</div>
      <div className="mt-1 text-lg font-bold font-mono" style={{ color }}>{value}</div>
    </div>
  );
}

const inputStyle = { background: '#0B0E11', border: '1px solid #2B3139', color: '#EAECEF' };

function buttonStyle(primary: boolean) {
  return primary
    ? { background: '#F0B90B', color: '#000', border: '1px solid #F0B90B' }
    : { background: '#1E2329', color: '#EAECEF', border: '1px solid #2B3139' };
}

function splitSymbols(raw: string) {
  return raw.split(',').map((item) => item.trim().toUpperCase()).filter(Boolean);
}

function formatTime(ms: number) {
  return ms ? new Date(ms).toLocaleDateString() : '--';
}

function statusColor(status: string) {
  switch (status) {
    case 'completed':
      return '#0ECB81';
    case 'failed':
    case 'cancelled':
      return '#F6465D';
    default:
      return '#F0B90B';
  }
}
