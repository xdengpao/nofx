use serde::{Deserialize, Serialize};
use crate::center::Center;
use crate::divergence::Divergence;
use crate::segment::Segment;
use crate::trend::TrendType;

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
pub enum SignalType {
    #[serde(rename = "buy1")]
    Buy1,
    #[serde(rename = "buy2")]
    Buy2,
    #[serde(rename = "buy3")]
    Buy3,
    #[serde(rename = "sell1")]
    Sell1,
    #[serde(rename = "sell2")]
    Sell2,
    #[serde(rename = "sell3")]
    Sell3,
    #[serde(rename = "quasi_buy2")]
    QuasiBuy2,
    #[serde(rename = "quasi_buy3")]
    QuasiBuy3,
    #[serde(rename = "quasi_sell2")]
    QuasiSell2,
    #[serde(rename = "quasi_sell3")]
    QuasiSell3,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Signal {
    pub signal_type: SignalType,
    pub direction: String,    // "long" or "short"
    pub price: f64,
    pub stop_loss: f64,
    pub take_profit: f64,
    pub confidence: i32,      // 0-100
    pub center_id: Option<usize>,
    pub divergence_strength: f64,
    pub timestamp: i64,
}

/// 从缠论结构中识别买卖点
pub fn detect_signals(
    segments: &[Segment],
    centers: &[Center],
    divergences: &[Divergence],
    trend: TrendType,
    current_price: f64,
) -> Vec<Signal> {
    let mut signals = Vec::new();

    // 一类买卖点：趋势背驰
    for div in divergences {
        let seg = segments.iter().find(|s| s.id == div.seg_c_id);
        let Some(seg) = seg else { continue };
        let center = centers.last();

        match div.div_type {
            crate::divergence::DivergenceType::Bottom if trend == TrendType::DownTrend => {
                let sl = seg.low * 0.98;
                let tp = center.map(|c| c.zg).unwrap_or(seg.high);
                signals.push(Signal {
                    signal_type: SignalType::Buy1,
                    direction: "long".into(),
                    price: current_price,
                    stop_loss: sl,
                    take_profit: tp,
                    confidence: (div.strength * 100.0).min(100.0) as i32,
                    center_id: center.map(|c| c.id),
                    divergence_strength: div.strength,
                    timestamp: seg.end_time,
                });
            }
            crate::divergence::DivergenceType::Top if trend == TrendType::UpTrend => {
                let sl = seg.high * 1.02;
                let tp = center.map(|c| c.zd).unwrap_or(seg.low);
                signals.push(Signal {
                    signal_type: SignalType::Sell1,
                    direction: "short".into(),
                    price: current_price,
                    stop_loss: sl,
                    take_profit: tp,
                    confidence: (div.strength * 100.0).min(100.0) as i32,
                    center_id: center.map(|c| c.id),
                    divergence_strength: div.strength,
                    timestamp: seg.end_time,
                });
            }
            _ => {}
        }
    }

    // 二类/三类买卖点：基于中枢位置
    if let Some(center) = centers.last() {
        if let Some(last_seg) = segments.last() {
            // 二类买点：回抽不入中枢
            if last_seg.low > center.zd && last_seg.low < center.zg && current_price > last_seg.low {
                signals.push(Signal {
                    signal_type: SignalType::Buy2,
                    direction: "long".into(),
                    price: current_price,
                    stop_loss: center.zd,
                    take_profit: center.high + (center.high - center.low),
                    confidence: 70,
                    center_id: Some(center.id),
                    divergence_strength: 0.0,
                    timestamp: last_seg.end_time,
                });
            }
            // 三类买点：离开中枢不回
            if last_seg.low > center.zg {
                signals.push(Signal {
                    signal_type: SignalType::Buy3,
                    direction: "long".into(),
                    price: current_price,
                    stop_loss: center.zg,
                    take_profit: current_price + (center.zg - center.zd),
                    confidence: 65,
                    center_id: Some(center.id),
                    divergence_strength: 0.0,
                    timestamp: last_seg.end_time,
                });
            }
        }
    }

    signals
}
