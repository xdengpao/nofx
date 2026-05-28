use serde::{Deserialize, Serialize};
use crate::center::Center;
use crate::divergence::Divergence;
use crate::kline::Direction;
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
            crate::divergence::DivergenceType::Top if trend == TrendType::UpTrend || trend == TrendType::Consolidation => {
                let conf_penalty = if trend == TrendType::Consolidation { 15 } else { 0 };
                let sl = seg.high * 1.02;
                let tp = center.map(|c| c.zd).unwrap_or(seg.low);
                signals.push(Signal {
                    signal_type: SignalType::Sell1,
                    direction: "short".into(),
                    price: current_price,
                    stop_loss: sl,
                    take_profit: tp,
                    confidence: ((div.strength * 100.0) as i32 - conf_penalty).clamp(0, 100),
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
            if last_seg.direction == Direction::Up && last_seg.low > center.zd && last_seg.low < center.zg && current_price > last_seg.low {
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
            // 二类卖点：反弹不破中枢上沿
            if last_seg.direction == Direction::Down
                && last_seg.high < center.zg
                && last_seg.high > center.zd
                && current_price < last_seg.high
            {
                signals.push(Signal {
                    signal_type: SignalType::Sell2,
                    direction: "short".into(),
                    price: current_price,
                    stop_loss: center.zg,
                    take_profit: (center.low - (center.high - center.low)).max(current_price * 0.9),
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
            // 三类卖点：跌破中枢不回
            if last_seg.direction == Direction::Down
                && last_seg.high < center.zd
                && current_price < center.zd
            {
                signals.push(Signal {
                    signal_type: SignalType::Sell3,
                    direction: "short".into(),
                    price: current_price,
                    stop_loss: center.zd,
                    take_profit: current_price - (center.zg - center.zd),
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

#[cfg(test)]
mod tests {
    use super::*;
    use crate::divergence::DivergenceType;

    fn segment(id: usize, direction: Direction, high: f64, low: f64) -> Segment {
        Segment {
            id,
            direction,
            strokes: vec![],
            high,
            low,
            start_time: id as i64,
            end_time: (id + 1) as i64,
        }
    }

    fn center() -> Center {
        Center {
            id: 3,
            zg: 110.0,
            zd: 100.0,
            high: 114.0,
            low: 96.0,
            segments: vec![0, 1, 2],
            start_time: 0,
            end_time: 3,
        }
    }

    fn signal_count(signals: &[Signal], signal_type: SignalType) -> usize {
        signals.iter().filter(|s| s.signal_type == signal_type).count()
    }

    #[test]
    fn sell2_requires_down_segment_and_uses_protected_target() {
        let segments = vec![segment(0, Direction::Down, 108.0, 96.0)];
        let signals = detect_signals(&segments, &[center()], &[], TrendType::DownTrend, 104.0);

        assert_eq!(signal_count(&signals, SignalType::Sell2), 1);
        let sell2 = signals.iter().find(|s| s.signal_type == SignalType::Sell2).unwrap();
        assert_eq!(sell2.direction, "short");
        assert_eq!(sell2.stop_loss, 110.0);
        assert!((sell2.take_profit - 93.6).abs() < 0.000001);
        assert_eq!(sell2.confidence, 70);
    }

    #[test]
    fn buy2_and_sell2_do_not_trigger_on_same_down_segment() {
        let segments = vec![segment(0, Direction::Down, 108.0, 104.0)];
        let signals = detect_signals(&segments, &[center()], &[], TrendType::Consolidation, 106.0);

        assert_eq!(signal_count(&signals, SignalType::Buy2), 0);
        assert_eq!(signal_count(&signals, SignalType::Sell2), 1);
    }

    #[test]
    fn buy2_still_triggers_on_up_segment() {
        let segments = vec![segment(0, Direction::Up, 108.0, 104.0)];
        let signals = detect_signals(&segments, &[center()], &[], TrendType::Consolidation, 106.0);

        assert_eq!(signal_count(&signals, SignalType::Buy2), 1);
        assert_eq!(signal_count(&signals, SignalType::Sell2), 0);
    }

    #[test]
    fn sell3_requires_down_segment_below_center() {
        let segments = vec![segment(0, Direction::Down, 98.0, 84.0)];
        let signals = detect_signals(&segments, &[center()], &[], TrendType::DownTrend, 94.0);

        assert_eq!(signal_count(&signals, SignalType::Sell3), 1);
        let sell3 = signals.iter().find(|s| s.signal_type == SignalType::Sell3).unwrap();
        assert_eq!(sell3.direction, "short");
        assert_eq!(sell3.stop_loss, 100.0);
        assert_eq!(sell3.take_profit, 84.0);
        assert_eq!(sell3.confidence, 65);
    }

    #[test]
    fn sell3_does_not_trigger_after_price_returns_above_zd() {
        let segments = vec![segment(0, Direction::Down, 98.0, 84.0)];
        let signals = detect_signals(&segments, &[center()], &[], TrendType::DownTrend, 101.0);

        assert_eq!(signal_count(&signals, SignalType::Sell3), 0);
    }

    #[test]
    fn sell1_triggers_in_consolidation_with_confidence_penalty() {
        let segments = vec![segment(7, Direction::Up, 120.0, 100.0)];
        let divergences = vec![Divergence {
            div_type: DivergenceType::Top,
            seg_a_id: 1,
            seg_c_id: 7,
            macd_ratio: 0.7,
            slope_ratio: 0.7,
            strength: 0.8,
        }];
        let signals = detect_signals(&segments, &[center()], &divergences, TrendType::Consolidation, 108.0);

        assert_eq!(signal_count(&signals, SignalType::Sell1), 1);
        let sell1 = signals.iter().find(|s| s.signal_type == SignalType::Sell1).unwrap();
        assert_eq!(sell1.direction, "short");
        assert_eq!(sell1.confidence, 65);
    }
}
