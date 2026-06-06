use serde::{Deserialize, Serialize};
use crate::fractal::{Fractal, FractalType};
use crate::kline::Direction;

/// 最少包含的合并K线数（顶底分型之间）
const MIN_STROKE_BARS: usize = 4;

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Stroke {
    pub id: usize,
    pub direction: Direction,
    pub start: Fractal,
    pub end: Fractal,
    pub high: f64,
    pub low: f64,
}

/// 从分型序列构建笔
/// 规则：顶底交替、相邻分型之间至少 MIN_STROKE_BARS 根合并K线
pub fn build_strokes(fractals: &[Fractal], min_bars: usize) -> Vec<Stroke> {
    let min_bars = if min_bars > 0 { min_bars } else { MIN_STROKE_BARS };
    if fractals.is_empty() {
        return vec![];
    }

    let mut strokes: Vec<Stroke> = Vec::new();
    let mut last_fx: Option<&Fractal> = None;
    let mut stroke_id = 0;

    for fx in fractals {
        let Some(prev) = last_fx else {
            last_fx = Some(fx);
            continue;
        };

        // 必须顶底交替
        if fx.fx_type == prev.fx_type {
            // 同类型分型：保留更极端的那个
            match fx.fx_type {
                FractalType::Top => {
                    if fx.price > prev.price {
                        last_fx = Some(fx);
                    }
                }
                FractalType::Bottom => {
                    if fx.price < prev.price {
                        last_fx = Some(fx);
                    }
                }
            }
            continue;
        }

        // 检查最少K线数
        if fx.index.saturating_sub(prev.index) < min_bars {
            continue;
        }

        // 价格有效性：顶必须高于底
        let (top_price, bot_price) = match prev.fx_type {
            FractalType::Top => (prev.price, fx.price),
            FractalType::Bottom => (fx.price, prev.price),
        };
        if top_price <= bot_price {
            continue;
        }

        let direction = match prev.fx_type {
            FractalType::Bottom => Direction::Up,
            FractalType::Top => Direction::Down,
        };

        strokes.push(Stroke {
            id: stroke_id,
            direction,
            start: prev.clone(),
            end: fx.clone(),
            high: top_price,
            low: bot_price,
        });
        stroke_id += 1;
        last_fx = Some(fx);
    }
    strokes
}

#[cfg(test)]
mod tests {
    use super::*;

    fn top(index: usize, price: f64) -> Fractal {
        Fractal { fx_type: FractalType::Top, index, price, open_time: index as i64, close_time: index as i64 }
    }
    fn bot(index: usize, price: f64) -> Fractal {
        Fractal { fx_type: FractalType::Bottom, index, price, open_time: index as i64, close_time: index as i64 }
    }

    #[test]
    fn test_basic_strokes() {
        let fractals = vec![bot(0, 100.0), top(5, 120.0), bot(10, 105.0), top(15, 130.0)];
        let strokes = build_strokes(&fractals, 4);
        assert_eq!(strokes.len(), 3);
        assert_eq!(strokes[0].direction, Direction::Up);
        assert_eq!(strokes[1].direction, Direction::Down);
        assert_eq!(strokes[2].direction, Direction::Up);
    }

    #[test]
    fn test_min_bars_filter() {
        // 分型间距不足
        let fractals = vec![bot(0, 100.0), top(2, 120.0), bot(4, 105.0)];
        let strokes = build_strokes(&fractals, 4);
        assert_eq!(strokes.len(), 0);
    }

    #[test]
    fn test_same_type_keeps_extreme() {
        // 连续两个底分型，保留更低的
        let fractals = vec![bot(0, 100.0), bot(3, 95.0), top(8, 120.0)];
        let strokes = build_strokes(&fractals, 4);
        assert_eq!(strokes.len(), 1);
        assert_eq!(strokes[0].start.price, 95.0);
    }
}
