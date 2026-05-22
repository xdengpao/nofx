use serde::{Deserialize, Serialize};
use crate::center::{self, Center};
use crate::contain;
use crate::divergence::{self, Divergence};
use crate::fractal::{self, Fractal};
use crate::kline::{Kline, MergedKline};
use crate::segment::{self, Segment};
use crate::signal::{self, Signal};
use crate::stroke::{self, Stroke};
use crate::trend::{self, TrendType};

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct AnalysisConfig {
    #[serde(default = "default_min_stroke_bars")]
    pub min_stroke_bars: usize,
    #[serde(default = "default_divergence_threshold")]
    pub divergence_threshold: f64,
    #[serde(default)]
    pub enable_extended_signals: bool,
    #[serde(default)]
    pub enable_recursive: bool,
    #[serde(default = "default_recursive_depth")]
    pub recursive_depth: usize,
}

fn default_min_stroke_bars() -> usize { 5 }
fn default_divergence_threshold() -> f64 { 0.8 }
fn default_recursive_depth() -> usize { 2 }

impl Default for AnalysisConfig {
    fn default() -> Self {
        Self {
            min_stroke_bars: 5,
            divergence_threshold: 0.8,
            enable_extended_signals: true,
            enable_recursive: true,
            recursive_depth: 2,
        }
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct AnalysisResult {
    pub merged_klines: Vec<MergedKline>,
    pub fractals: Vec<Fractal>,
    pub strokes: Vec<Stroke>,
    pub segments: Vec<Segment>,
    pub centers: Vec<Center>,
    pub trend: TrendType,
    pub divergences: Vec<Divergence>,
    pub signals: Vec<Signal>,
}

/// 对单个时间周期的K线执行完整缠论分析
pub fn analyze(klines: &[Kline], macd_hist: &[f64], config: &AnalysisConfig) -> AnalysisResult {
    let merged = contain::process_contain(klines);
    let fractals = fractal::find_fractals(&merged);
    let strokes = stroke::build_strokes(&fractals, config.min_stroke_bars);
    let segments = segment::build_segments(&strokes);
    let centers = center::build_centers(&segments);
    let trend_type = trend::classify_trend(&centers);

    // 背驰检测：比较最后两个同向段
    let mut divergences = Vec::new();
    if segments.len() >= 3 {
        let last = &segments[segments.len() - 1];
        // 找同方向的前一段
        for i in (0..segments.len() - 2).rev() {
            if segments[i].direction == last.direction {
                if let Some(div) = divergence::detect_divergence(
                    &segments[i], last, macd_hist, config.divergence_threshold,
                ) {
                    divergences.push(div);
                }
                break;
            }
        }
    }

    let current_price = klines.last().map(|k| k.close).unwrap_or(0.0);
    let signals = signal::detect_signals(&segments, &centers, &divergences, trend_type, current_price);

    AnalysisResult { merged_klines: merged, fractals, strokes, segments, centers, trend: trend_type, divergences, signals }
}
