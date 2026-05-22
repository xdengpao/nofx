use serde::{Deserialize, Serialize};
use crate::kline::Direction;
use crate::stroke::Stroke;

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Segment {
    pub id: usize,
    pub direction: Direction,
    pub strokes: Vec<Stroke>,
    pub high: f64,
    pub low: f64,
    pub start_time: i64,
    pub end_time: i64,
}

/// 特征序列元素：取同向笔的高低点构成
#[derive(Debug, Clone)]
struct CharElement {
    high: f64,
    low: f64,
    stroke_idx: usize,
}

/// 从笔序列构建线段（特征序列方法）
/// 规则：线段至少由3笔组成，线段终结需要特征序列出现分型
pub fn build_segments(strokes: &[Stroke]) -> Vec<Segment> {
    if strokes.len() < 3 {
        return vec![];
    }

    let mut segments: Vec<Segment> = Vec::new();
    let mut seg_start = 0;
    let mut seg_id = 0;

    loop {
        if seg_start + 2 >= strokes.len() {
            break;
        }
        // 当前线段方向 = 第一笔方向
        let seg_dir = strokes[seg_start].direction;

        // 寻找线段终结点：从第3笔开始检查特征序列分型
        let mut seg_end = seg_start + 2; // 最少3笔
        let mut terminated = false;

        while seg_end < strokes.len() {
            // 构建反向笔的特征序列（取与线段方向相反的笔）
            if has_characteristic_fractal(strokes, seg_start, seg_end, seg_dir) {
                terminated = true;
                break;
            }
            seg_end += 1;
        }

        if !terminated {
            seg_end = strokes.len() - 1;
        }

        let seg_strokes = strokes[seg_start..=seg_end].to_vec();
        let high = seg_strokes.iter().map(|s| s.high).fold(f64::NEG_INFINITY, f64::max);
        let low = seg_strokes.iter().map(|s| s.low).fold(f64::INFINITY, f64::min);

        segments.push(Segment {
            id: seg_id,
            direction: seg_dir,
            strokes: seg_strokes,
            high,
            low,
            start_time: strokes[seg_start].start.open_time,
            end_time: strokes[seg_end].end.close_time,
        });
        seg_id += 1;
        seg_start = seg_end;

        if seg_start >= strokes.len() - 1 {
            break;
        }
    }
    segments
}

/// 检查从 seg_start 到 check_end 的笔序列中，反向笔的特征序列是否出现分型
fn has_characteristic_fractal(strokes: &[Stroke], seg_start: usize, check_end: usize, seg_dir: Direction) -> bool {
    // 收集反向笔（与线段方向相反的笔）作为特征序列元素
    let elements: Vec<CharElement> = strokes[seg_start..=check_end]
        .iter()
        .enumerate()
        .filter(|(_, s)| s.direction != seg_dir)
        .map(|(i, s)| CharElement {
            high: s.high,
            low: s.low,
            stroke_idx: seg_start + i,
        })
        .collect();

    if elements.len() < 3 {
        return false;
    }

    // 对特征序列做包含关系处理后检查分型
    let processed = process_char_contain(&elements, seg_dir);
    if processed.len() < 3 {
        return false;
    }

    // 检查最后3个元素是否形成分型
    let n = processed.len();
    let (a, b, c) = (&processed[n - 3], &processed[n - 2], &processed[n - 1]);

    match seg_dir {
        Direction::Up => {
            // 向上线段终结：特征序列出现顶分型
            b.high > a.high && b.high > c.high
        }
        Direction::Down => {
            // 向下线段终结：特征序列出现底分型
            b.low < a.low && b.low < c.low
        }
        _ => false,
    }
}

/// 特征序列的包含关系处理
fn process_char_contain(elements: &[CharElement], seg_dir: Direction) -> Vec<CharElement> {
    if elements.is_empty() {
        return vec![];
    }
    let mut result = vec![elements[0].clone()];
    for i in 1..elements.len() {
        let e = &elements[i];
        let last = result.last_mut().unwrap();
        // 包含关系
        if (last.high >= e.high && last.low <= e.low) || (e.high >= last.high && e.low <= last.low) {
            match seg_dir {
                Direction::Up => {
                    last.high = last.high.max(e.high);
                    last.low = last.low.max(e.low);
                }
                _ => {
                    last.high = last.high.min(e.high);
                    last.low = last.low.min(e.low);
                }
            }
        } else {
            result.push(e.clone());
        }
    }
    result
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::fractal::{Fractal, FractalType};

    fn make_stroke(id: usize, dir: Direction, high: f64, low: f64, start_idx: usize, end_idx: usize) -> Stroke {
        Stroke {
            id,
            direction: dir,
            start: Fractal { fx_type: FractalType::Bottom, index: start_idx, price: low, open_time: start_idx as i64, close_time: start_idx as i64 },
            end: Fractal { fx_type: FractalType::Top, index: end_idx, price: high, open_time: end_idx as i64, close_time: end_idx as i64 },
            high,
            low,
        }
    }

    #[test]
    fn test_min_3_strokes() {
        let strokes = vec![
            make_stroke(0, Direction::Up, 110.0, 100.0, 0, 5),
            make_stroke(1, Direction::Down, 108.0, 95.0, 5, 10),
        ];
        let segs = build_segments(&strokes);
        assert_eq!(segs.len(), 0);
    }

    #[test]
    fn test_basic_segment() {
        let strokes = vec![
            make_stroke(0, Direction::Up, 110.0, 100.0, 0, 5),
            make_stroke(1, Direction::Down, 108.0, 103.0, 5, 10),
            make_stroke(2, Direction::Up, 115.0, 105.0, 10, 15),
            make_stroke(3, Direction::Down, 112.0, 98.0, 15, 20),
            make_stroke(4, Direction::Up, 105.0, 96.0, 20, 25),
        ];
        let segs = build_segments(&strokes);
        assert!(!segs.is_empty());
        assert_eq!(segs[0].direction, Direction::Up);
    }

    #[test]
    fn test_segment_high_low() {
        let strokes = vec![
            make_stroke(0, Direction::Up, 110.0, 100.0, 0, 5),
            make_stroke(1, Direction::Down, 108.0, 103.0, 5, 10),
            make_stroke(2, Direction::Up, 120.0, 106.0, 10, 15),
        ];
        let segs = build_segments(&strokes);
        if !segs.is_empty() {
            assert_eq!(segs[0].high, 120.0);
            assert_eq!(segs[0].low, 100.0);
        }
    }
}
