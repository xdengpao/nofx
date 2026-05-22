use serde::{Deserialize, Serialize};
use crate::segment::Segment;

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
pub enum DivergenceType {
    #[serde(rename = "top_divergence")]
    Top,
    #[serde(rename = "bottom_divergence")]
    Bottom,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Divergence {
    pub div_type: DivergenceType,
    pub seg_a_id: usize,
    pub seg_c_id: usize,
    pub macd_ratio: f64,   // C段MACD面积 / A段MACD面积
    pub slope_ratio: f64,  // C段斜率 / A段斜率
    pub strength: f64,     // 综合背驰强度 0-1
}

/// MACD 柱状图面积（绝对值累加）
pub fn macd_area(macd_hist: &[f64], start: usize, end: usize) -> f64 {
    if start >= end || end > macd_hist.len() {
        return 0.0;
    }
    macd_hist[start..end].iter().map(|v| v.abs()).sum()
}

/// 价格斜率（每根K线的平均变化）
pub fn price_slope(seg: &Segment) -> f64 {
    let bars = seg.strokes.last().map(|s| s.end.index).unwrap_or(0)
        .saturating_sub(seg.strokes.first().map(|s| s.start.index).unwrap_or(0));
    if bars == 0 {
        return 0.0;
    }
    let price_change = (seg.high - seg.low).abs();
    price_change / bars as f64
}

/// 检测背驰（双模式：MACD面积 + 趋势力度）
pub fn detect_divergence(
    seg_a: &Segment,
    seg_c: &Segment,
    macd_hist: &[f64],
    threshold: f64,
) -> Option<Divergence> {
    let a_start = seg_a.strokes.first()?.start.index;
    let a_end = seg_a.strokes.last()?.end.index;
    let c_start = seg_c.strokes.first()?.start.index;
    let c_end = seg_c.strokes.last()?.end.index;

    let area_a = macd_area(macd_hist, a_start, a_end);
    let area_c = macd_area(macd_hist, c_start, c_end);
    let macd_ratio = if area_a > 0.0 { area_c / area_a } else { 1.0 };

    let slope_a = price_slope(seg_a);
    let slope_c = price_slope(seg_c);
    let slope_ratio = if slope_a > 0.0 { slope_c / slope_a } else { 1.0 };

    // 综合强度：两个指标加权
    let strength = 1.0 - (macd_ratio * 0.6 + slope_ratio * 0.4);

    if macd_ratio < threshold || slope_ratio < threshold {
        let div_type = if seg_c.high > seg_a.high {
            DivergenceType::Top
        } else {
            DivergenceType::Bottom
        };
        Some(Divergence { div_type, seg_a_id: seg_a.id, seg_c_id: seg_c.id, macd_ratio, slope_ratio, strength })
    } else {
        None
    }
}
