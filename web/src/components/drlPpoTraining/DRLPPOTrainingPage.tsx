import { useEffect, useMemo, useState } from 'react';
import useSWR from 'swr';
import { drlPpoTrainApi } from '../../lib/drlPpoTrainApi';
import type {
  DRLPPOEvaluation,
  DRLPPOGapCheck,
  DRLPPOHealth,
  DRLPPOJob,
  DRLPPOStagingConfig,
} from '../../types/drlPpoTraining';

const panelStyle = { background: '#181A20', border: '1px solid #2B3139' };
const innerStyle = { background: '#0B0E11', border: '1px solid #2B3139' };
const inputStyle = { background: '#0B0E11', border: '1px solid #2B3139', color: '#EAECEF' };
const tooltipStyle = { background: '#0B0E11', border: '1px solid #2B3139', color: '#EAECEF' };

const TRAINING_PARAM_HELP = {
  totalTimesteps: 'PPO与训练环境交互的总步数。数值越大，策略学习时间越长、训练更慢；过小通常只能做冒烟验证，过大可能在单一区间过拟合。',
  observationWindow: '每次决策输入的历史K线根数。例如1h周期下60表示看最近60小时。窗口越大上下文越多，但训练更慢，且可能稀释短周期信号。',
  initialBalance: '训练/回测环境的虚拟初始USDT余额，用于权益曲线、仓位和收益率计算，不会影响真实账户资金。',
  outputModelName: '模型产物名称前缀，用于区分训练结果、ONNX文件和后续部署候选。建议包含交易对、周期、训练目的或版本。',
  takerFee: '吃单手续费率。0.0005表示0.05%。该成本会进入训练环境，数值越高，策略越倾向减少高频或低边际交易。',
  makerFee: '挂单手续费率。0.0002表示0.02%。用于模拟成交成本；如果策略环境没有区分 maker/taker，也会作为成本参数保留。',
  slippage: '滑点比例。0.0003表示0.03%。用于模拟下单成交价偏移，数值越高越保守，能降低回测过度乐观。',
  nSteps: 'PPO每轮策略更新前采样的环境步数。较大通常梯度更稳定但占用更多内存、更新更慢；较小响应快但噪声更大。',
  batchSize: 'PPO优化时的mini-batch大小。通常应能被n_steps整除或接近整除；过小噪声大，过大可能降低更新频率。',
  nEpochs: '每批采样数据重复优化的轮数。更大能更充分利用样本，但过大容易过拟合近期样本或让策略更新过猛。',
  rolling: '启用滚动窗口训练/评估，用多个时间片验证策略稳定性。适合检查不同市场阶段的鲁棒性，但训练耗时更长。',
  allowIncompleteData: '允许历史覆盖不足时仍启动训练。仅建议排查流程或临时实验使用；真实模型训练应保持关闭，避免缺口导致样本偏差。',
};

type JobFilter = 'all' | 'active' | 'completed' | 'failed';

interface DRLPPOTrainingPageProps {
  health?: DRLPPOHealth;
}

export function DRLPPOTrainingPage({ health }: DRLPPOTrainingPageProps) {
  const [source, setSource] = useState('binance-futures');
  const [symbol, setSymbol] = useState('BTCUSDT');
  const [timeframe, setTimeframe] = useState('1h');
  const [dataFrom, setDataFrom] = useState('2026-01-01');
  const [dataTo, setDataTo] = useState('2026-02-01');
  const [gapCheck, setGapCheck] = useState<DRLPPOGapCheck | null>(null);
  const [fetchRunId, setFetchRunId] = useState('');
  const [historyMessage, setHistoryMessage] = useState('');
  const [checkingGaps, setCheckingGaps] = useState(false);

  const [totalTimesteps, setTotalTimesteps] = useState(10_000);
  const [observationWindow, setObservationWindow] = useState(60);
  const [initialBalance, setInitialBalance] = useState(10_000);
  const [takerFee, setTakerFee] = useState(0.0005);
  const [makerFee, setMakerFee] = useState(0.0002);
  const [slippage, setSlippage] = useState(0.0003);
  const [nSteps, setNSteps] = useState(2048);
  const [batchSize, setBatchSize] = useState(64);
  const [nEpochs, setNEpochs] = useState(10);
  const [rolling, setRolling] = useState(false);
  const [allowIncompleteData, setAllowIncompleteData] = useState(false);
  const [outputModelName, setOutputModelName] = useState('btc_ppo_smoke');
  const [trainMessage, setTrainMessage] = useState('');

  const [jobFilter, setJobFilter] = useState<JobFilter>('all');
  const [selectedJobId, setSelectedJobId] = useState('');
  const [logStream, setLogStream] = useState<'stdout' | 'stderr'>('stdout');
  const [logContent, setLogContent] = useState('');
  const [logOffset, setLogOffset] = useState(0);
  const [logLoading, setLogLoading] = useState(false);

  const [selectedModelId, setSelectedModelId] = useState('');
  const [evaluation, setEvaluation] = useState<DRLPPOEvaluation | null>(null);
  const [stagingConfig, setStagingConfig] = useState<DRLPPOStagingConfig | null>(null);
  const [modelMessage, setModelMessage] = useState('');

  const { data: storage, mutate: refreshStorage } = useSWR('drl-ppo-storage', drlPpoTrainApi.storage, {
    refreshInterval: 30000,
    revalidateOnFocus: false,
  });
  const { data: marketCatalog, error: marketsError, mutate: refreshMarkets } = useSWR('drl-ppo-markets', () => drlPpoTrainApi.markets(false), {
    refreshInterval: 10 * 60 * 1000,
    revalidateOnFocus: false,
  });
  const { data: coverage, mutate: refreshCoverage } = useSWR(
    `drl-ppo-coverage-${source}`,
    () => drlPpoTrainApi.coverage(source),
    { refreshInterval: 30000, revalidateOnFocus: false },
  );
  const { data: fetchJob } = useSWR(
    fetchRunId ? `drl-ppo-history-fetch-${fetchRunId}` : null,
    () => drlPpoTrainApi.historyFetchJob(fetchRunId),
    { refreshInterval: fetchRunId ? 3000 : 0, revalidateOnFocus: false },
  );
  const { data: jobs, mutate: refreshJobs } = useSWR('drl-ppo-jobs', drlPpoTrainApi.jobs, {
    refreshInterval: 3000,
    revalidateOnFocus: false,
  });
  const { data: models, mutate: refreshModels } = useSWR('drl-ppo-models', drlPpoTrainApi.models, {
    refreshInterval: 10000,
    revalidateOnFocus: false,
  });

  const marketSources = useMemo(() => {
    if (marketCatalog?.sources?.length) return marketCatalog.sources;
    return [{
      source: 'binance-futures',
      display_name: 'Binance USD-M Futures',
      exchange: 'binance',
      timeframes: ['3m', '15m', '1h', '4h'],
      symbols: [{ symbol: 'BTCUSDT', base_asset: 'BTC', quote_asset: 'USDT', status: 'TRADING', contract_type: 'PERPETUAL' }],
      synced_at: '',
    }];
  }, [marketCatalog]);

  const selectedMarketSource = useMemo(() => {
    return marketSources.find((item) => item.source === source) ?? marketSources[0];
  }, [marketSources, source]);

  const sourceOptions = useMemo(() => marketSources.map((item) => ({
    value: item.source,
    label: `${item.source} · ${item.display_name}`,
  })), [marketSources]);

  const symbolOptions = useMemo(() => (selectedMarketSource?.symbols ?? []).map((item) => ({
    value: item.symbol,
    label: `${item.symbol}${item.base_asset && item.quote_asset ? ` · ${item.base_asset}/${item.quote_asset}` : ''}`,
  })), [selectedMarketSource]);

  const timeframeOptions = selectedMarketSource?.timeframes?.length ? selectedMarketSource.timeframes : ['3m', '15m', '1h', '4h'];

  const matchingCoverage = useMemo(() => {
    return (coverage ?? []).find((item) => (
      item.source === source &&
      item.symbol === symbol.toUpperCase() &&
      item.timeframe === timeframe
    ));
  }, [coverage, source, symbol, timeframe]);

  const visibleJobs = useMemo(() => {
    const list = jobs ?? [];
    if (jobFilter === 'active') return list.filter((job) => isActiveStatus(job.status));
    if (jobFilter === 'completed') return list.filter((job) => job.status === 'completed');
    if (jobFilter === 'failed') return list.filter((job) => ['failed', 'cancelled', 'interrupted'].includes(job.status));
    return list;
  }, [jobs, jobFilter]);

  const selectedJob = useMemo(() => {
    return (jobs ?? []).find((job) => job.job_id === selectedJobId);
  }, [jobs, selectedJobId]);

  const selectedModel = useMemo(() => {
    return (models ?? []).find((model) => model.model_id === selectedModelId);
  }, [models, selectedModelId]);

  const trainingEnvironmentReady = Boolean(health?.enabled && health?.training_ready);
  const coverageTrainingAllowed = Boolean(gapCheck?.ok || allowIncompleteData);
  const trainingAllowed = trainingEnvironmentReady && coverageTrainingAllowed;
  const fetchSummary = fetchJob?.fetch_summary;

  useEffect(() => {
    if (marketSources.length > 0 && !marketSources.some((item) => item.source === source)) {
      setSource(marketSources[0].source);
    }
  }, [marketSources, source]);

  useEffect(() => {
    if (!selectedMarketSource) return;
    const symbols = selectedMarketSource.symbols.map((item) => item.symbol);
    if (symbols.length > 0 && !symbols.includes(symbol.toUpperCase())) {
      setSymbol(symbols.includes('BTCUSDT') ? 'BTCUSDT' : symbols[0]);
    }
  }, [selectedMarketSource, symbol]);

  useEffect(() => {
    if (timeframeOptions.length > 0 && !timeframeOptions.includes(timeframe)) {
      setTimeframe(timeframeOptions.includes('1h') ? '1h' : timeframeOptions[0]);
    }
  }, [timeframeOptions, timeframe]);

  useEffect(() => {
    setGapCheck(null);
  }, [source, symbol, timeframe, dataFrom, dataTo]);

  useEffect(() => {
    if ((fetchJob?.status === 'completed' || fetchJob?.status === 'failed') && fetchRunId) {
      refreshCoverage();
    }
  }, [fetchJob?.status, fetchRunId, refreshCoverage]);

  useEffect(() => {
    if (!selectedJobId && jobs && jobs.length > 0) {
      setSelectedJobId(jobs[0].job_id);
    }
  }, [jobs, selectedJobId]);

  useEffect(() => {
    if (!selectedModelId && models && models.length > 0) {
      setSelectedModelId(models[0].model_id);
    }
  }, [models, selectedModelId]);

  useEffect(() => {
    if (!selectedJobId) {
      setLogContent('');
      setLogOffset(0);
      return;
    }
    let ignore = false;
    setLogLoading(true);
    drlPpoTrainApi.logs(selectedJobId, logStream, 0, 8192)
      .then((chunk) => {
        if (ignore) return;
        setLogContent(chunk.content);
        setLogOffset(chunk.next_offset);
      })
      .catch((err) => {
        if (!ignore) setLogContent(String(err.message || err));
      })
      .finally(() => {
        if (!ignore) setLogLoading(false);
      });
    return () => {
      ignore = true;
    };
  }, [selectedJobId, logStream]);

  useEffect(() => {
    setEvaluation(null);
    setStagingConfig(null);
    setModelMessage('');
    if (!selectedModel?.evaluation_path || !selectedModelId) return;
    let ignore = false;
    drlPpoTrainApi.evaluation(selectedModelId)
      .then((result) => {
        if (!ignore) setEvaluation(result);
      })
      .catch(() => undefined);
    return () => {
      ignore = true;
    };
  }, [selectedModel?.evaluation_path, selectedModelId]);

  const checkGaps = async () => {
    setCheckingGaps(true);
    setHistoryMessage('');
    const fromDate = normalizeDateValue(dataFrom);
    const toDate = normalizeDateValue(dataTo);
    try {
      const result = await drlPpoTrainApi.gaps({
        source,
        symbol: symbol.toUpperCase(),
        timeframe,
        from: fromDate,
        to: toDate,
        timezone: 'Asia/Singapore',
      });
      setDataFrom(fromDate);
      setDataTo(toDate);
      setGapCheck(result);
      setHistoryMessage(result.ok ? '覆盖检查通过' : `覆盖不足: ${result.detail || '存在缺口'}`);
      try {
        await refreshCoverage();
      } catch {
        // 覆盖状态刷新失败不应覆盖缺口检查结果。
      }
    } catch (err) {
      setHistoryMessage(errorMessage(err));
    } finally {
      setCheckingGaps(false);
    }
  };

  const startFetch = async () => {
    setHistoryMessage('');
    if (gapCheck && gapCheck.ok && !window.confirm('当前区间已覆盖，仍要重新补数据吗？')) return;
    const fromDate = normalizeDateValue(dataFrom);
    const toDate = normalizeDateValue(dataTo);
    try {
      const job = await drlPpoTrainApi.fetchHistory({
        source,
        symbols: [symbol.toUpperCase()],
        timeframes: [timeframe],
        data_from: fromDate,
        data_to: toDate,
        timezone: 'Asia/Singapore',
        rate_limit: { requests_per_minute: 120, concurrency: 1, page_limit: 1000, max_retries: 3 },
      });
      setDataFrom(fromDate);
      setDataTo(toDate);
      setFetchRunId(job.run_id);
      setHistoryMessage(`补数据任务已提交: ${job.run_id}`);
    } catch (err) {
      setHistoryMessage(errorMessage(err));
    }
  };

  const createTrainingJob = async () => {
    setTrainMessage('');
    if (!trainingEnvironmentReady) {
      setTrainMessage(`DRL-PPO训练环境不可用: ${(health?.environment_errors ?? []).join('；') || 'Python或训练脚本未就绪'}`);
      return;
    }
    if (!trainingAllowed) {
      setTrainMessage('请先完成覆盖检查，或勾选允许不完整数据训练。');
      return;
    }
    if (!gapCheck?.ok && allowIncompleteData && !window.confirm('历史数据覆盖不足，确认启动不完整数据训练？')) return;
    const fromDate = normalizeDateValue(dataFrom);
    const toDate = normalizeDateValue(dataTo);
    try {
      const job = await drlPpoTrainApi.createJob({
        source,
        symbol: symbol.toUpperCase(),
        timeframe,
        start: fromDate,
        end: toDate,
        total_timesteps: totalTimesteps,
        n_steps: nSteps > 0 ? nSteps : undefined,
        batch_size: batchSize > 0 ? batchSize : undefined,
        n_epochs: nEpochs > 0 ? nEpochs : undefined,
        observation_window: observationWindow,
        initial_balance: initialBalance,
        taker_fee: takerFee,
        maker_fee: makerFee,
        slippage,
        rolling,
        output_model_name: outputModelName.trim(),
        allow_incomplete_data: allowIncompleteData,
      });
      setDataFrom(fromDate);
      setDataTo(toDate);
      setSelectedJobId(job.job_id);
      setTrainMessage(`训练任务已提交: ${job.job_id}`);
      refreshJobs();
    } catch (err) {
      setTrainMessage(errorMessage(err));
    }
  };

  const cancelSelectedJob = async (job: DRLPPOJob) => {
    if (!window.confirm(`确认取消训练任务 ${job.job_id}？`)) return;
    try {
      await drlPpoTrainApi.cancelJob(job.job_id);
      refreshJobs();
    } catch (err) {
      setTrainMessage(errorMessage(err));
    }
  };

  const loadNextLogs = async () => {
    if (!selectedJobId) return;
    setLogLoading(true);
    try {
      const chunk = await drlPpoTrainApi.logs(selectedJobId, logStream, logOffset, 0);
      setLogContent((current) => current + chunk.content);
      setLogOffset(chunk.next_offset);
    } catch (err) {
      setLogContent((current) => `${current}\n${errorMessage(err)}`);
    } finally {
      setLogLoading(false);
    }
  };

  const evaluateSelectedModel = async () => {
    if (!selectedModelId) return;
    setModelMessage('');
    try {
      const result = await drlPpoTrainApi.evaluateModel(selectedModelId, {
        source: selectedModel?.source || source,
        symbol: selectedModel?.symbol || symbol.toUpperCase(),
        timeframe: selectedModel?.timeframe || timeframe,
        start: selectedModel?.data_from || normalizeDateValue(dataFrom),
        end: selectedModel?.data_to || normalizeDateValue(dataTo),
        observation_window: selectedModel?.observation_window || observationWindow,
      });
      setEvaluation(result);
      setModelMessage('评估完成');
      refreshModels();
    } catch (err) {
      setModelMessage(errorMessage(err));
    }
  };

  const loadStagingConfig = async () => {
    if (!selectedModelId) return;
    setModelMessage('');
    try {
      const result = await drlPpoTrainApi.stagingConfig(selectedModelId);
      setStagingConfig(result);
      setModelMessage('staging config 已生成，仅供人工确认');
    } catch (err) {
      setModelMessage(errorMessage(err));
    }
  };

  return (
    <div className="space-y-5">
      <div className="flex flex-col gap-2 xl:flex-row xl:items-end xl:justify-between">
        <div>
          <div className="text-xs font-mono" style={{ color: '#F0B90B' }}>
            DRL-PPO · training_api={health?.enabled ? 'enabled' : 'unknown'} · python={health?.python_bin || 'python3'} · onnx_runtime={health?.onnx_runtime || 'stub'}
          </div>
          <h1 className="text-2xl font-bold mt-1">DRL-PPO 策略训练</h1>
        </div>
        <div className="grid grid-cols-2 md:grid-cols-4 gap-2 text-xs">
          <StatusPill label="训练API" active={Boolean(health?.enabled)} />
          <StatusPill label="Python" active={Boolean(health?.python_available)} />
          <StatusPill label="训练环境" active={trainingEnvironmentReady} />
          <StatusPill label="覆盖检查" active={Boolean(gapCheck?.ok)} />
        </div>
      </div>

      <div className="grid grid-cols-1 2xl:grid-cols-[420px_1fr] gap-5">
        <div className="space-y-5">
          <section className="rounded p-4" style={panelStyle}>
            <div className="flex items-center justify-between gap-3 mb-3">
              <h2 className="font-bold">Storage 状态</h2>
              <button className="px-3 py-1.5 rounded text-xs font-semibold" style={buttonStyle(false)} onClick={() => refreshStorage()}>
                刷新
              </button>
            </div>
            <div className="grid grid-cols-2 gap-3">
              <Metric label="Root 来源" value={storage?.root_source || '--'} />
              <Metric label="可用空间" value={formatBytes(storage?.disk.free_bytes)} tone={storage && storage.disk.free_bytes > 0 ? 'good' : undefined} />
              <Metric label="已用空间" value={formatBytes(storage?.disk.used_bytes)} />
              <Metric label="外部路径" value={storage?.allow_external_paths ? 'allowed' : 'locked'} tone={storage?.allow_external_paths ? 'bad' : 'good'} />
            </div>
            <div className="mt-3 rounded p-3 text-xs" style={innerStyle}>
              <div style={{ color: '#848E9C' }}>Compose 挂载</div>
              <div className="mt-1 font-mono" style={{ color: '#EAECEF' }}>{composeHint(storage)}</div>
              <div className="mt-1 font-mono" style={{ color: '#848E9C' }}>容量来源: {storage?.disk.source || 'statfs'}</div>
            </div>
            {(storage?.warnings ?? []).map((warning) => (
              <div key={warning} className="mt-3 rounded p-3 text-xs" style={{ background: 'rgba(240, 185, 11, 0.08)', border: '1px solid rgba(240, 185, 11, 0.25)', color: '#F0B90B' }}>
                {warning}
              </div>
            ))}
            <div className="mt-4 max-h-72 overflow-auto space-y-2">
              {(storage?.directories ?? []).map((dir) => (
                <div key={dir.name} className="grid grid-cols-[1fr_auto] gap-3 rounded p-2 text-xs" style={innerStyle}>
                  <div className="min-w-0">
                    <div className="font-mono truncate" style={{ color: '#EAECEF' }}>{dir.name}</div>
                    <div className="font-mono truncate" style={{ color: '#848E9C' }}>{safePath(dir.path)}</div>
                    {dir.error && <div className="mt-1" style={{ color: '#F6465D' }}>{dir.error}</div>}
                  </div>
                  <div className="text-right">
                    <div style={{ color: dir.exists && dir.writable ? '#0ECB81' : '#F6465D' }}>{dir.exists && dir.writable ? 'ready' : 'check'}</div>
                    <div className="font-mono mt-1" style={{ color: '#848E9C' }}>{formatBytes(dir.bytes)}</div>
                  </div>
                </div>
              ))}
            </div>
          </section>

          <section className="rounded p-4" style={panelStyle}>
            <div className="flex items-center justify-between gap-3 mb-3">
              <div>
                <h2 className="font-bold">历史数据覆盖</h2>
                <div className="mt-1 text-xs" style={{ color: marketsError ? '#F6465D' : '#848E9C' }}>
                  {marketsError
                    ? `标的同步失败，使用兜底选项: ${errorMessage(marketsError)}`
                    : `已同步 ${selectedMarketSource?.symbols.length ?? 0} 个标的 · ${formatDateTime(selectedMarketSource?.synced_at || marketCatalog?.synced_at)}`}
                </div>
              </div>
              <button
                className="px-3 py-1.5 rounded text-xs font-semibold"
                style={buttonStyle(false)}
                onClick={() => refreshMarkets(drlPpoTrainApi.markets(true), { revalidate: false })}
              >
                同步标的
              </button>
            </div>
            <SelectField label="source" value={source} onChange={setSource} options={sourceOptions} />
            <SelectField label="symbol" value={symbol} onChange={(value) => setSymbol(value.toUpperCase())} options={symbolOptions} />
            <SelectField label="timeframe" value={timeframe} onChange={setTimeframe} options={timeframeOptions} />
            <div className="grid grid-cols-2 gap-3">
              <DateField label="from" value={dataFrom} onChange={setDataFrom} max={dataTo || todayDateValue()} />
              <DateField label="to" value={dataTo} onChange={setDataTo} min={dataFrom} max={todayDateValue()} />
            </div>
            <div className="mt-3 grid grid-cols-2 gap-2">
              <button className="px-3 py-2 rounded text-sm font-semibold" style={buttonStyle(false, checkingGaps)} onClick={checkGaps} disabled={checkingGaps}>
                检查缺口
              </button>
              <button className="px-3 py-2 rounded text-sm font-semibold" style={buttonStyle(true)} onClick={startFetch}>
                补历史数据
              </button>
            </div>
            {historyMessage && <div className="mt-3 text-xs font-mono" style={{ color: gapCheck?.ok ? '#0ECB81' : '#F0B90B' }}>{historyMessage}</div>}
            <div className="mt-4 rounded p-3 text-xs" style={innerStyle}>
              <div className="flex items-center justify-between gap-3">
                <span style={{ color: '#848E9C' }}>当前覆盖</span>
                <span style={{ color: gapCheck?.ok ? '#0ECB81' : '#F0B90B' }}>{gapCheck ? (gapCheck.ok ? '覆盖充足' : '覆盖不足') : '未检查'}</span>
              </div>
              <div className="mt-2 font-mono" style={{ color: '#EAECEF' }}>
                {matchingCoverage
                  ? `${matchingCoverage.count} candles · ${formatMs(matchingCoverage.from_ms)} -> ${formatMs(matchingCoverage.to_ms)}`
                  : '未找到匹配 coverage'}
              </div>
              {matchingCoverage?.data_hash && (
                <div className="mt-1 font-mono truncate" style={{ color: '#848E9C' }}>hash {matchingCoverage.data_hash}</div>
              )}
              {gapCheck && (
                <div className="mt-2 font-mono" style={{ color: (gapCheck.gaps ?? []).length > 0 ? '#F6465D' : '#0ECB81' }}>
                  缺失数据: {(gapCheck.gaps ?? []).length > 0 ? `${gapCheck.gaps.length} 段` : '无'}
                </div>
              )}
              {(gapCheck?.gaps ?? []).slice(0, 5).map((gap) => (
                <div key={gap.id || `${gap.start_time_ms}-${gap.end_time_ms}`} className="mt-2" style={{ color: '#F6465D' }}>
                  {gap.issue_type} · {formatMs(gap.start_time_ms)}{' -> '}{formatMs(gap.end_time_ms)} · {gap.detail}
                </div>
              ))}
            </div>
            {fetchJob && (
              <div className="mt-4 rounded p-3 text-xs" style={innerStyle}>
                <div className="flex items-center justify-between gap-3">
                  <span className="font-mono">{fetchJob.run_id}</span>
                  <span style={{ color: statusColor(fetchJob.status) }}>{fetchJob.status}</span>
                </div>
                <div className="mt-3 grid grid-cols-2 gap-2">
                  <Metric label="inserted" value={String(fetchSummary?.inserted_count ?? fetchJob.progress?.executions ?? 0)} tone="good" compact />
                  <Metric label="duplicate" value={String(fetchSummary?.duplicate_count ?? 0)} compact />
                  <Metric label="requests" value={String(fetchSummary?.request_count ?? 0)} compact />
                  <Metric label="failed" value={String(Object.keys(fetchSummary?.failed ?? {}).length)} tone={Object.keys(fetchSummary?.failed ?? {}).length > 0 ? 'bad' : undefined} compact />
                </div>
                {fetchSummary?.failed && Object.keys(fetchSummary.failed).length > 0 && (
                  <pre className="mt-3 max-h-28 overflow-auto text-xs whitespace-pre-wrap" style={{ color: '#F6465D' }}>
                    {JSON.stringify(fetchSummary.failed, null, 2)}
                  </pre>
                )}
              </div>
            )}
          </section>
        </div>

        <div className="space-y-5">
          <section className="rounded p-4" style={panelStyle}>
            <div className="flex flex-col gap-3 xl:flex-row xl:items-start xl:justify-between">
              <div>
                <h2 className="font-bold">训练配置</h2>
                <div className="mt-1 text-xs" style={{ color: '#848E9C' }}>
                  覆盖不足时默认禁用训练；手动允许不完整数据会提交 allow_incomplete_data。
                </div>
              </div>
              <div className="text-xs font-mono" style={{ color: trainingAllowed ? '#0ECB81' : '#F0B90B' }}>
                {!trainingEnvironmentReady ? 'environment_not_ready' : trainingAllowed ? 'ready' : 'waiting_coverage_check'}
              </div>
            </div>
            {!trainingEnvironmentReady && (
              <div className="mt-3 rounded p-3 text-xs" style={{ background: 'rgba(246, 70, 93, 0.08)', border: '1px solid rgba(246, 70, 93, 0.25)', color: '#F6465D' }}>
                {(health?.environment_errors ?? []).join('；') || 'Python或训练脚本未就绪'}
              </div>
            )}
            <div className="mt-4 grid grid-cols-1 md:grid-cols-2 xl:grid-cols-4 gap-3">
              <NumberField label="total_timesteps" value={totalTimesteps} onChange={setTotalTimesteps} min={1000} help={TRAINING_PARAM_HELP.totalTimesteps} />
              <NumberField label="observation_window" value={observationWindow} onChange={setObservationWindow} min={10} max={200} help={TRAINING_PARAM_HELP.observationWindow} />
              <NumberField label="initial_balance" value={initialBalance} onChange={setInitialBalance} min={1} help={TRAINING_PARAM_HELP.initialBalance} />
              <Field label="output_model_name" value={outputModelName} onChange={setOutputModelName} help={TRAINING_PARAM_HELP.outputModelName} />
              <NumberField label="taker_fee" value={takerFee} onChange={setTakerFee} step={0.0001} min={0} help={TRAINING_PARAM_HELP.takerFee} />
              <NumberField label="maker_fee" value={makerFee} onChange={setMakerFee} step={0.0001} min={0} help={TRAINING_PARAM_HELP.makerFee} />
              <NumberField label="slippage" value={slippage} onChange={setSlippage} step={0.0001} min={0} help={TRAINING_PARAM_HELP.slippage} />
              <NumberField label="n_steps" value={nSteps} onChange={setNSteps} min={0} help={TRAINING_PARAM_HELP.nSteps} />
              <NumberField label="batch_size" value={batchSize} onChange={setBatchSize} min={0} help={TRAINING_PARAM_HELP.batchSize} />
              <NumberField label="n_epochs" value={nEpochs} onChange={setNEpochs} min={0} help={TRAINING_PARAM_HELP.nEpochs} />
              <ToggleField label="rolling" value={rolling} onChange={setRolling} help={TRAINING_PARAM_HELP.rolling} />
              <ToggleField label="允许不完整数据" value={allowIncompleteData} onChange={setAllowIncompleteData} help={TRAINING_PARAM_HELP.allowIncompleteData} />
            </div>
            <div className="mt-4 flex flex-col gap-2 sm:flex-row sm:items-center">
              <button className="px-4 py-2 rounded text-sm font-semibold" style={buttonStyle(true, !trainingAllowed)} onClick={createTrainingJob} disabled={!trainingAllowed}>
                启动训练
              </button>
              {trainMessage && <div className="text-xs font-mono" style={{ color: trainMessage.includes('失败') || trainMessage.includes('请先') || trainMessage.includes('不可用') ? '#F6465D' : '#F0B90B' }}>{trainMessage}</div>}
            </div>
          </section>

          <section className="rounded p-4" style={panelStyle}>
            <div className="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between mb-3">
              <h2 className="font-bold">训练任务</h2>
              <div className="flex gap-2 flex-wrap">
                {(['all', 'active', 'completed', 'failed'] as JobFilter[]).map((filter) => (
                  <button
                    key={filter}
                    className="px-3 py-1.5 rounded text-xs font-semibold"
                    style={buttonStyle(jobFilter === filter)}
                    onClick={() => setJobFilter(filter)}
                  >
                    {filter}
                  </button>
                ))}
                <button className="px-3 py-1.5 rounded text-xs font-semibold" style={buttonStyle(false)} onClick={() => refreshJobs()}>
                  刷新
                </button>
              </div>
            </div>
            <div className="grid grid-cols-1 xl:grid-cols-[minmax(280px,420px)_1fr] gap-4">
              <div className="space-y-2 max-h-[520px] overflow-auto">
                {visibleJobs.length === 0 && <EmptyText text="暂无训练任务" />}
                {visibleJobs.map((job) => (
                  <button
                    key={job.job_id}
                    className="block w-full rounded p-3 text-left text-xs transition-all"
                    style={selectedJobId === job.job_id ? { ...innerStyle, border: '1px solid #F0B90B' } : innerStyle}
                    onClick={() => setSelectedJobId(job.job_id)}
                  >
                    <div className="flex items-center justify-between gap-3">
                      <span className="font-mono font-bold truncate">{job.job_id}</span>
                      <span style={{ color: statusColor(job.status) }}>{job.status}</span>
                    </div>
                    <div className="mt-2 grid grid-cols-3 gap-2 font-mono" style={{ color: '#848E9C' }}>
                      <span>{job.request.symbol}</span>
                      <span>{job.request.timeframe}</span>
                      <span>{progressText(job)}</span>
                    </div>
                    <ProgressBar value={progressPercent(job)} />
                  </button>
                ))}
              </div>
              <div className="space-y-4 min-w-0">
                {selectedJob ? (
                  <>
                    <div className="rounded p-3 text-xs" style={innerStyle}>
                      <div className="flex flex-col gap-2 md:flex-row md:items-center md:justify-between">
                        <div className="min-w-0">
                          <div className="font-mono font-bold truncate">{selectedJob.job_id}</div>
                          <div className="mt-1 font-mono" style={{ color: '#848E9C' }}>
                            {selectedJob.request.symbol} {selectedJob.request.timeframe} · {selectedJob.request.start}{' -> '}{selectedJob.request.end}
                          </div>
                        </div>
                        {isActiveStatus(selectedJob.status) && (
                          <button className="px-3 py-1.5 rounded text-xs font-semibold" style={buttonStyle(false)} onClick={() => cancelSelectedJob(selectedJob)}>
                            取消
                          </button>
                        )}
                      </div>
                      {selectedJob.error && <div className="mt-3" style={{ color: '#F6465D' }}>{selectedJob.error}</div>}
                      <div className="mt-3 grid grid-cols-2 md:grid-cols-4 gap-2">
                        <Metric label="timesteps" value={`${selectedJob.progress?.timesteps ?? 0}`} compact />
                        <Metric label="target" value={`${selectedJob.progress?.total_timesteps ?? selectedJob.request.total_timesteps}`} compact />
                        <Metric label="runtime" value={`${selectedJob.progress?.runtime_seconds ?? 0}s`} compact />
                        <Metric label="stalled" value={selectedJob.progress?.stalled ? 'yes' : 'no'} tone={selectedJob.progress?.stalled ? 'bad' : undefined} compact />
                      </div>
                    </div>
                    <div className="rounded p-3" style={innerStyle}>
                      <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between mb-2">
                        <div className="flex gap-2">
                          <button className="px-3 py-1.5 rounded text-xs font-semibold" style={buttonStyle(logStream === 'stdout')} onClick={() => setLogStream('stdout')}>stdout</button>
                          <button className="px-3 py-1.5 rounded text-xs font-semibold" style={buttonStyle(logStream === 'stderr')} onClick={() => setLogStream('stderr')}>stderr</button>
                        </div>
                        <button className="px-3 py-1.5 rounded text-xs font-semibold" style={buttonStyle(false, logLoading)} onClick={loadNextLogs} disabled={logLoading}>
                          加载增量
                        </button>
                      </div>
                      <pre className="h-72 overflow-auto whitespace-pre-wrap break-words rounded p-3 text-xs font-mono" style={{ background: '#05070A', color: '#B7BDC6', border: '1px solid #2B3139' }}>
                        {logContent || '暂无日志'}
                      </pre>
                      <div className="mt-2 text-xs font-mono" style={{ color: '#848E9C' }}>offset {logOffset}</div>
                    </div>
                  </>
                ) : (
                  <EmptyText text="选择训练任务查看详情与日志" />
                )}
              </div>
            </div>
          </section>

          <section className="rounded p-4" style={panelStyle}>
            <div className="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between mb-3">
              <h2 className="font-bold">模型评估与部署准备</h2>
              <button className="px-3 py-1.5 rounded text-xs font-semibold" style={buttonStyle(false)} onClick={() => refreshModels()}>
                刷新模型
              </button>
            </div>
            <div className="grid grid-cols-1 xl:grid-cols-[360px_1fr] gap-4">
              <div className="space-y-2 max-h-[420px] overflow-auto">
                {(models ?? []).length === 0 && <EmptyText text="暂无模型产物" />}
                {(models ?? []).map((model) => (
                  <button
                    key={model.model_id}
                    className="block w-full rounded p-3 text-left text-xs"
                    style={selectedModelId === model.model_id ? { ...innerStyle, border: '1px solid #F0B90B' } : innerStyle}
                    onClick={() => setSelectedModelId(model.model_id)}
                  >
                    <div className="flex items-center justify-between gap-3">
                      <span className="font-mono font-bold truncate">{model.model_id}</span>
                      <span style={{ color: model.deployable ? '#0ECB81' : '#F0B90B' }}>{model.deployable ? 'deployable' : 'review'}</span>
                    </div>
                    <div className="mt-2 font-mono" style={{ color: '#848E9C' }}>
                      {model.symbol || '--'} {model.timeframe || '--'} · {model.total_timesteps ?? 0} steps
                    </div>
                    <div className="mt-1 font-mono truncate" style={{ color: '#848E9C' }}>
                      zip {safePath(model.zip_path)} · onnx {safePath(model.onnx_path)}
                    </div>
                  </button>
                ))}
              </div>
              <div className="space-y-4 min-w-0">
                {selectedModel ? (
                  <>
                    <div className="rounded p-3 text-xs" style={innerStyle}>
                      <div className="flex flex-col gap-2 md:flex-row md:items-start md:justify-between">
                        <div className="min-w-0">
                          <div className="font-mono font-bold truncate">{selectedModel.model_id}</div>
                          <div className="mt-1" style={{ color: '#848E9C' }}>{selectedModel.runtime_note || '当前默认推理后端为stub，部署前需确认ONNX Runtime。'}</div>
                        </div>
                        <div className="flex gap-2">
                          <button className="px-3 py-1.5 rounded text-xs font-semibold" style={buttonStyle(false)} onClick={evaluateSelectedModel}>
                            评估
                          </button>
                          <button className="px-3 py-1.5 rounded text-xs font-semibold" style={buttonStyle(true)} onClick={loadStagingConfig}>
                            staging config
                          </button>
                        </div>
                      </div>
                      <div className="mt-3 grid grid-cols-2 md:grid-cols-4 gap-2">
                        <Metric label="version" value={selectedModel.model_version || '--'} compact />
                        <Metric label="zip" value={safePath(selectedModel.zip_path)} compact />
                        <Metric label="onnx" value={safePath(selectedModel.onnx_path)} compact />
                        <Metric label="evaluation" value={safePath(selectedModel.evaluation_path)} compact />
                      </div>
                      {modelMessage && <div className="mt-3 text-xs font-mono" style={{ color: modelMessage.includes('失败') ? '#F6465D' : '#F0B90B' }}>{modelMessage}</div>}
                    </div>
                    {evaluation && (
                      <div className="rounded p-3" style={innerStyle}>
                        <h3 className="font-bold text-sm mb-3">评估报告</h3>
                        <div className="grid grid-cols-2 md:grid-cols-4 gap-2">
                          <Metric label="annual_return" value={formatRatio(evaluation.annual_return)} compact />
                          <Metric label="sharpe" value={formatNumber(evaluation.sharpe)} compact />
                          <Metric label="max_drawdown" value={formatRatio(evaluation.max_drawdown)} compact />
                          <Metric label="direction" value={formatRatio(evaluation.directional_accuracy)} compact />
                          <Metric label="win_rate" value={formatRatio(evaluation.win_rate)} compact />
                          <Metric label="deploy" value={evaluation.recommended_for_deploy ? 'yes' : 'no'} tone={evaluation.recommended_for_deploy ? 'good' : 'bad'} compact />
                        </div>
                      </div>
                    )}
                    {stagingConfig && (
                      <div className="rounded p-3" style={innerStyle}>
                        <h3 className="font-bold text-sm mb-3">Staging Config</h3>
                        {(stagingConfig.warnings ?? []).map((warning) => (
                          <div key={warning} className="mb-2 text-xs" style={{ color: '#F0B90B' }}>{warning}</div>
                        ))}
                        <pre className="max-h-80 overflow-auto whitespace-pre-wrap break-words rounded p-3 text-xs font-mono" style={{ background: '#05070A', color: '#B7BDC6', border: '1px solid #2B3139' }}>
                          {JSON.stringify(stagingConfig.trader_patch, null, 2)}
                        </pre>
                      </div>
                    )}
                  </>
                ) : (
                  <EmptyText text="选择模型查看评估与 staging config" />
                )}
              </div>
            </div>
          </section>
        </div>
      </div>
    </div>
  );
}

function Field({ label, value, onChange, help }: { label: string; value: string; onChange: (value: string) => void; help?: string }) {
  return (
    <label className="block text-xs mt-3" style={{ color: '#848E9C' }}>
      <LabelWithHelp label={label} help={help} />
      <input value={value} onChange={(event) => onChange(event.target.value)} className="mt-1 w-full rounded px-3 py-2 text-sm font-mono" style={inputStyle} />
    </label>
  );
}

function DateField({ label, value, onChange, min, max }: {
  label: string;
  value: string;
  onChange: (value: string) => void;
  min?: string;
  max?: string;
}) {
  return (
    <label className="block text-xs mt-3" style={{ color: '#848E9C' }}>
      {label}
      <input
        type="date"
        value={normalizeDateValue(value)}
        min={min ? normalizeDateValue(min) : undefined}
        max={max ? normalizeDateValue(max) : undefined}
        onChange={(event) => onChange(normalizeDateValue(event.target.value))}
        className="mt-1 w-full rounded px-3 py-2 text-sm font-mono"
        style={inputStyle}
      />
    </label>
  );
}

function NumberField({ label, value, onChange, min, max, step, help }: {
  label: string;
  value: number;
  onChange: (value: number) => void;
  min?: number;
  max?: number;
  step?: number;
  help?: string;
}) {
  return (
    <label className="block text-xs" style={{ color: '#848E9C' }}>
      <LabelWithHelp label={label} help={help} />
      <input
        type="number"
        value={value}
        min={min}
        max={max}
        step={step ?? 1}
        onChange={(event) => onChange(Number(event.target.value))}
        className="mt-1 w-full rounded px-3 py-2 text-sm font-mono"
        style={inputStyle}
      />
    </label>
  );
}

type SelectOption = string | { value: string; label: string };

function SelectField({ label, value, onChange, options }: {
  label: string;
  value: string;
  onChange: (value: string) => void;
  options: SelectOption[];
}) {
  return (
    <label className="block text-xs mt-3" style={{ color: '#848E9C' }}>
      {label}
      <select value={value} onChange={(event) => onChange(event.target.value)} className="mt-1 w-full rounded px-3 py-2 text-sm font-mono" style={inputStyle}>
        {options.map((option) => {
          const optionValue = typeof option === 'string' ? option : option.value;
          const optionLabel = typeof option === 'string' ? option : option.label;
          return <option key={optionValue} value={optionValue}>{optionLabel}</option>;
        })}
      </select>
    </label>
  );
}

function ToggleField({ label, value, onChange, help }: { label: string; value: boolean; onChange: (value: boolean) => void; help?: string }) {
  return (
    <label className="flex items-center justify-between gap-3 rounded px-3 py-2 text-xs" style={{ ...innerStyle, color: '#848E9C' }}>
      <LabelWithHelp label={label} help={help} />
      <input type="checkbox" checked={value} onChange={(event) => onChange(event.target.checked)} />
    </label>
  );
}

function LabelWithHelp({ label, help }: { label: string; help?: string }) {
  if (!help) return <span>{label}</span>;
  return (
    <span className="inline-flex min-w-0 items-center gap-1.5">
      <span className="truncate">{label}</span>
      <span className="relative inline-flex group">
        <span
          tabIndex={0}
          role="button"
          aria-label={`${label} 参数说明`}
          title={help}
          className="inline-flex h-4 w-4 items-center justify-center rounded-full text-[10px] font-bold outline-none"
          style={{ background: '#1E2329', border: '1px solid #5E6673', color: '#B7BDC6' }}
        >
          ?
        </span>
        <span
          className="pointer-events-none invisible absolute left-1/2 top-5 z-30 w-72 -translate-x-1/2 rounded px-3 py-2 text-xs leading-relaxed opacity-0 shadow-xl transition-opacity group-hover:visible group-hover:opacity-100 group-focus-within:visible group-focus-within:opacity-100"
          style={tooltipStyle}
        >
          {help}
        </span>
      </span>
    </span>
  );
}

function Metric({ label, value, tone, compact }: { label: string; value?: string; tone?: 'good' | 'bad'; compact?: boolean }) {
  const color = tone === 'good' ? '#0ECB81' : tone === 'bad' ? '#F6465D' : '#EAECEF';
  return (
    <div className="rounded p-3 min-w-0" style={innerStyle}>
      <div className="text-xs truncate" style={{ color: '#848E9C' }}>{label}</div>
      <div className={`${compact ? 'text-sm' : 'text-lg'} mt-1 font-bold font-mono truncate`} style={{ color }}>{value || '--'}</div>
    </div>
  );
}

function StatusPill({ label, active }: { label: string; active: boolean }) {
  return (
    <div
      className="rounded px-3 py-2 font-mono"
      style={active
        ? { background: 'rgba(14, 203, 129, 0.1)', color: '#0ECB81', border: '1px solid rgba(14, 203, 129, 0.2)' }
        : { background: 'rgba(246, 70, 93, 0.08)', color: '#F6465D', border: '1px solid rgba(246, 70, 93, 0.18)' }
      }
    >
      {label}: {active ? 'on' : 'off'}
    </div>
  );
}

function EmptyText({ text }: { text: string }) {
  return (
    <div className="rounded p-4 text-sm" style={{ ...innerStyle, color: '#848E9C' }}>
      {text}
    </div>
  );
}

function ProgressBar({ value }: { value: number }) {
  return (
    <div className="mt-3 h-2 overflow-hidden rounded" style={{ background: '#1E2329' }}>
      <div className="h-full" style={{ width: `${Math.max(0, Math.min(100, value))}%`, background: '#F0B90B' }} />
    </div>
  );
}

function buttonStyle(primary: boolean, disabled = false) {
  if (disabled) {
    return { background: '#1E2329', color: '#5E6673', border: '1px solid #2B3139', cursor: 'not-allowed' };
  }
  return primary
    ? { background: '#F0B90B', color: '#000', border: '1px solid #F0B90B' }
    : { background: '#1E2329', color: '#EAECEF', border: '1px solid #2B3139' };
}

function composeHint(storage: { host_mount_path?: string; container_root?: string; host_mount_source?: string } | undefined) {
  if (!storage) return '--';
  if (!storage.host_mount_path && !storage.container_root) return '原生运行，未检测到 Compose 挂载环境变量';
  const containerRoot = storage.container_root || '/app/runtime';
  if (storage.host_mount_path && !storage.host_mount_path.startsWith('/')) {
    return `${storage.host_mount_path} -> ${containerRoot}`;
  }
  if (storage.host_mount_path) {
    return `宿主机路径已通过 ${storage.host_mount_source || 'env'} 覆盖 -> ${containerRoot}`;
  }
  return `容器内 ${containerRoot}`;
}

function safePath(path?: string) {
  const value = String(path || '').trim();
  if (!value) return '--';
  if (value.startsWith('/app/runtime/')) return value.slice('/app/runtime/'.length);
  if (value === '/app/runtime') return 'Storage Root';
  if (value.startsWith('/')) {
    const parts = value.split('/').filter(Boolean);
    return parts[parts.length - 1] || 'Storage Root';
  }
  return value;
}

function formatBytes(value?: number) {
  if (!value || value <= 0) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  let size = value;
  let index = 0;
  while (size >= 1024 && index < units.length - 1) {
    size /= 1024;
    index++;
  }
  return `${size.toFixed(index === 0 ? 0 : 1)} ${units[index]}`;
}

function formatMs(value?: number) {
  if (!value) return '--';
  return new Date(value).toLocaleString();
}

function normalizeDateValue(value: string) {
  const raw = String(value || '').trim();
  const match = raw.match(/^(\d{4})-(\d{1,2})-(\d{1,2})$/);
  if (!match) return raw;
  return `${match[1]}-${match[2].padStart(2, '0')}-${match[3].padStart(2, '0')}`;
}

function todayDateValue() {
  const today = new Date();
  const year = today.getFullYear();
  const month = String(today.getMonth() + 1).padStart(2, '0');
  const day = String(today.getDate()).padStart(2, '0');
  return `${year}-${month}-${day}`;
}

function formatDateTime(value?: string) {
  if (!value) return '--';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return '--';
  return date.toLocaleString();
}

function formatNumber(value: unknown) {
  return typeof value === 'number' && Number.isFinite(value) ? value.toFixed(3) : '--';
}

function formatRatio(value: unknown) {
  return typeof value === 'number' && Number.isFinite(value) ? `${(value * 100).toFixed(2)}%` : '--';
}

function isActiveStatus(status: string) {
  return status === 'pending' || status === 'running';
}

function progressPercent(job: DRLPPOJob) {
  const current = job.progress?.timesteps ?? 0;
  const total = job.progress?.total_timesteps || job.request.total_timesteps || 0;
  if (total <= 0) return 0;
  return (current / total) * 100;
}

function progressText(job: DRLPPOJob) {
  const current = job.progress?.timesteps ?? 0;
  const total = job.progress?.total_timesteps || job.request.total_timesteps || 0;
  if (total <= 0) return '0%';
  return `${Math.round((current / total) * 100)}%`;
}

function statusColor(status: string) {
  switch (status) {
    case 'completed':
      return '#0ECB81';
    case 'failed':
    case 'cancelled':
    case 'interrupted':
      return '#F6465D';
    default:
      return '#F0B90B';
  }
}

function errorMessage(err: unknown) {
  return err instanceof Error ? err.message : String(err);
}
