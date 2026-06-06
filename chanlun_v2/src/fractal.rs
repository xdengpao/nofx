use serde::{Deserialize, Serialize};
use crate::kline::MergedKline;

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
pub enum FractalType {
    #[serde(rename = "top")]
    Top,
    #[serde(rename = "bottom")]
    Bottom,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Fractal {
    pub fx_type: FractalType,
    pub index: usize,       // 在 merged klines 中的位置
    pub price: f64,         // 顶分型取 high，底分型取 low
    pub open_time: i64,
    pub close_time: i64,
}

/// 从合并后的K线序列中识别分型
pub fn find_fractals(merged: &[MergedKline]) -> Vec<Fractal> {
    if merged.len() < 3 {
        return vec![];
    }
    let mut fractals = Vec::new();
    for i in 1..merged.len() - 1 {
        let prev = &merged[i - 1];
        let curr = &merged[i];
        let next = &merged[i + 1];

        if curr.high > prev.high && curr.high > next.high
            && curr.low > prev.low && curr.low > next.low
        {
            fractals.push(Fractal {
                fx_type: FractalType::Top,
                index: i,
                price: curr.high,
                open_time: curr.open_time,
                close_time: curr.close_time,
            });
        } else if curr.low < prev.low && curr.low < next.low
            && curr.high < prev.high && curr.high < next.high
        {
            fractals.push(Fractal {
                fx_type: FractalType::Bottom,
                index: i,
                price: curr.low,
                open_time: curr.open_time,
                close_time: curr.close_time,
            });
        }
    }
    fractals
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::kline::Direction;

    fn mk(index: usize, high: f64, low: f64) -> MergedKline {
        MergedKline { index, open_time: index as i64, close_time: index as i64, high, low, direction: Direction::Neutral, merged_count: 1 }
    }

    #[test]
    fn test_top_fractal() {
        let merged = vec![mk(0, 10.0, 5.0), mk(1, 15.0, 8.0), mk(2, 12.0, 6.0)];
        let fx = find_fractals(&merged);
        assert_eq!(fx.len(), 1);
        assert_eq!(fx[0].fx_type, FractalType::Top);
        assert_eq!(fx[0].price, 15.0);
    }

    #[test]
    fn test_bottom_fractal() {
        let merged = vec![mk(0, 15.0, 10.0), mk(1, 12.0, 5.0), mk(2, 14.0, 8.0)];
        let fx = find_fractals(&merged);
        assert_eq!(fx.len(), 1);
        assert_eq!(fx[0].fx_type, FractalType::Bottom);
        assert_eq!(fx[0].price, 5.0);
    }

    #[test]
    fn test_alternating() {
        let merged = vec![
            mk(0, 10.0, 5.0), mk(1, 15.0, 8.0), mk(2, 12.0, 6.0),
            mk(3, 8.0, 3.0), mk(4, 11.0, 7.0),
        ];
        let fx = find_fractals(&merged);
        assert_eq!(fx.len(), 2);
        assert_eq!(fx[0].fx_type, FractalType::Top);
        assert_eq!(fx[1].fx_type, FractalType::Bottom);
    }
}
