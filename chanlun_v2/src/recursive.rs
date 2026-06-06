use crate::analyzer::AnalysisResult;
use crate::signal::Signal;
use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct MultiLevelResult {
    pub levels: Vec<(String, AnalysisResult)>, // (timeframe, result)
    pub refined_signals: Vec<Signal>,
}

/// 区间套递归：高级别定方向，低级别精确点位
pub fn recursive_refine(levels: &[(String, AnalysisResult)]) -> Vec<Signal> {
    if levels.is_empty() {
        return vec![];
    }
    // 取最低级别的信号，用高级别过滤
    let lowest = &levels[levels.len() - 1].1;
    let mut refined = lowest.signals.clone();

    // 高级别趋势过滤
    if levels.len() >= 2 {
        let higher_trend = levels[0].1.trend;
        refined.retain(|s| {
            match (s.direction.as_str(), higher_trend) {
                ("long", crate::trend::TrendType::DownTrend) => {
                    // 逆势信号降低置信度但不完全过滤
                    true
                }
                ("short", crate::trend::TrendType::UpTrend) => true,
                _ => true,
            }
        });
        // 逆势惩罚
        for s in &mut refined {
            let aligned = match (s.direction.as_str(), higher_trend) {
                ("long", crate::trend::TrendType::UpTrend) => true,
                ("short", crate::trend::TrendType::DownTrend) => true,
                _ => false,
            };
            if !aligned {
                s.confidence = (s.confidence - 30).max(0);
            } else {
                s.confidence = (s.confidence + 15).min(100);
            }
        }
    }
    refined
}
